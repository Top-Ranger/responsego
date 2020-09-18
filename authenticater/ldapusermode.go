// +build ldap

// SPDX-License-Identifier: Apache-2.0
// Copyright 2020 Marcus Soll
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	  http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package authenticater

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"time"

	"golang.org/x/time/rate"

	"github.com/Top-Ranger/responsego/registry"
	"github.com/go-ldap/ldap/v3"
)

func init() {
	err := registry.RegisterAuthenticater(&LDAPUserMode{}, "LDAP-usermode")
	if err != nil {
		panic(err)
	}
}

// LDAPUserMode is an authenticator for using LDAP in user mode.
// It creates a new connection for every call to Authenticate and tries to bind the user.
type LDAPUserMode struct {
	// The endpoint of the LDAP server. Supports ldap://, ldaps://, ldapi://
	Endpoint string

	// Whether to use StartTLS. Must be disabled on encrypted connections.
	UseStartTLS bool

	// Pattern for the initial bind. Must contain a single %s which is replaced by the username.
	BindUserPattern string

	// Time limit for the LDAP search.
	TimeLimit int

	// Number of requests allowed per second.
	// Value 0 represents no rate limit.
	RateLimit int

	// Search base dn used for searching user DN.
	BaseDN string

	// Filter used in LDAP search to find the user. Must contain a single %s which is replaced by the username.
	LDAPUserFilter string

	// If set to true, certificate validation will be skipped.
	// Only set this to true if you absolutely must and have a secure connection, otherwise user data (including passwords) might be leaked!
	// If you are unsure, set it to false.
	InsecureSkipCertificateVerify bool

	limit rate.Limiter
}

// LoadConfig loads the LDAP configuration as a JSON.
func (l *LDAPUserMode) LoadConfig(b []byte) error {
	err := json.Unmarshal(b, l)
	if err != nil {
		return err
	}

	// Test connection
	conn, err := ldap.DialURL(l.Endpoint, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: l.InsecureSkipCertificateVerify}))
	if err != nil {
		return err
	}
	defer conn.Close()

	if l.UseStartTLS {
		err = conn.StartTLS(nil)
		if err != nil {
			return err
		}
	}

	if l.RateLimit == 0 {
		l.limit.SetLimit(rate.Inf)
	} else {
		l.limit.SetLimit(rate.Limit(l.RateLimit))
		l.limit.SetBurst(l.RateLimit)
	}

	return nil
}

// Authenticate verifies a user / password combination by binding it to the LDAP server.
func (l *LDAPUserMode) Authenticate(user, password string) (bool, error) {
	// Rate limit
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(l.TimeLimit)*time.Second)
	defer cancel()
	err := l.limit.Wait(ctx)
	if err != nil {
		return false, err
	}

	// Connect
	conn, err := ldap.DialURL(l.Endpoint, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: l.InsecureSkipCertificateVerify}))
	if err != nil {
		return false, err
	}
	defer conn.Close()

	if l.UseStartTLS {
		err = conn.StartTLS(nil)
		if err != nil {
			return false, err
		}
	}

	err = conn.Bind(fmt.Sprintf(l.BindUserPattern, user), password)
	if err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) { // This is an
			return false, nil
		}
		return false, err
	}

	// Get User
	searchRequest := ldap.NewSearchRequest(
		l.BaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, l.TimeLimit, false,
		fmt.Sprintf(l.LDAPUserFilter, ldap.EscapeFilter(user)),
		[]string{"dn"},
		nil,
	)

	searchResults, err := conn.Search(searchRequest)
	if err != nil {
		return false, err
	}

	if len(searchResults.Entries) != 1 {
		if len(searchResults.Entries) == 0 {
			// nothing found, that's ok
			return false, nil
		}
		return false, fmt.Errorf("LDAP: Wrong number of entries (%d)", len(searchResults.Entries))
	}

	dn := searchResults.Entries[0].DN

	// Bind to user
	err = conn.Bind(dn, password)
	if err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}
