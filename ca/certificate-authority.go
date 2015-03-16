// Copyright 2014 ISRG.  All rights reserved
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package ca

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"time"

	"github.com/letsencrypt/boulder/core"

	"github.com/cloudflare/cfssl/auth"
	"github.com/cloudflare/cfssl/config"
	"github.com/cloudflare/cfssl/signer"
	"github.com/cloudflare/cfssl/signer/remote"
)

type CertificateAuthorityImpl struct {
	profile string
	Signer  signer.Signer
	SA      core.StorageAuthority
}

// NewCertificateAuthorityImpl creates a CA that talks to a remote CFSSL
// instance.  (To use a local signer, simply instantiate CertificateAuthorityImpl
// directly.)  Communications with the CA are authenticated with MACs,
// using CFSSL's authenticated signature scheme.  A CA created in this way
// issues for a single profile on the remote signer, which is indicated
// by name in this constructor.
func NewCertificateAuthorityImpl(hostport string, authKey string, profile string) (ca *CertificateAuthorityImpl, err error) {
	// Create the remote signer
	localProfile := config.SigningProfile{
		Expiry:     60 * time.Minute, // BOGUS: Required by CFSSL, but not used
		RemoteName: hostport,
	}

	localProfile.Provider, err = auth.New(authKey, nil)
	if err != nil {
		return
	}

	signer, err := remote.NewSigner(&config.Signing{Default: &localProfile})
	if err != nil {
		return
	}

	ca = &CertificateAuthorityImpl{Signer: signer, profile: profile}
	return
}

func (ca *CertificateAuthorityImpl) IssueCertificate(csr x509.CertificateRequest) (cert core.Certificate, err error) {
	// XXX Take in authorizations and verify that union covers CSR?
	// Pull hostnames from CSR
	hostNames := csr.DNSNames // DNSNames + CN from CSR
	if len(hostNames) < 1 {
		err = errors.New("Cannot issue a certificate without a hostname.")
		return
	}
	var commonName string
	if len(csr.Subject.CommonName) > 0 {
		commonName = csr.Subject.CommonName
	} else {
		commonName = hostNames[0]
	}

	// Convert the CSR to PEM
	csrPEM := string(pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE REQUEST",
		Bytes: csr.Raw,
	}))

	// Send the cert off for signing
	req := signer.SignRequest{
		Request: csrPEM,
		Profile: ca.profile,
		Hosts:   hostNames,
		Subject: &signer.Subject{
			CN: commonName,
		},
	}
	certPEM, err := ca.Signer.Sign(req)
	if err != nil {
		return
	}

	if len(certPEM) == 0 {
		err = errors.New("No certificate returned by server")
		return
	}

	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		err = errors.New("Invalid certificate value returned")
		return
	}
	certDER := block.Bytes

	// Store the cert with the certificate authority, if provided
	certID, err := ca.SA.AddCertificate(certDER)
	if err != nil {
		return
	}

	cert = core.Certificate{
		ID:     certID,
		DER:    certDER,
		Status: core.StatusValid,
	}
	return
}