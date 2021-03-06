// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package daemon

import (
	"net/http"
	"strconv"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/polkit"
)

var polkitCheckAuthorization = polkit.CheckAuthorization

var checkPolkitAction = checkPolkitActionImpl

func checkPolkitActionImpl(r *http.Request, ucred *ucrednet, action string) *apiError {
	var flags polkit.CheckFlags
	allowHeader := r.Header.Get(client.AllowInteractionHeader)
	if allowHeader != "" {
		if allow, err := strconv.ParseBool(allowHeader); err != nil {
			logger.Noticef("error parsing %s header: %s", client.AllowInteractionHeader, err)
		} else if allow {
			flags |= polkit.CheckAllowInteraction
		}
	}
	// Pass both pid and uid from the peer ucred to avoid pid race
	switch authorized, err := polkitCheckAuthorization(ucred.Pid, ucred.Uid, action, nil, flags); err {
	case nil:
		if authorized {
			// polkit says user is authorised
			return nil
		}
	case polkit.ErrDismissed:
		return AuthCancelled("cancelled")
	default:
		logger.Noticef("polkit error: %s", err)
	}
	return Unauthorized("access denied")
}

// accessChecker checks whether a particular request is allowed.
//
// An access checker will either allow a request, deny it, or return
// accessUnknown, which indicates the decision should be delegated to
// the next access checker.
type accessChecker interface {
	CheckAccess(r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError
}

// requireSnapdSocket ensures the request was received via snapd.socket.
func requireSnapdSocket(ucred *ucrednet) *apiError {
	if ucred == nil {
		return Forbidden("access denied")
	}

	if ucred.Socket != dirs.SnapdSocket {
		return Forbidden("access denied")
	}

	return nil
}

// openAccess allows requests without authentication, provided they
// have peer credentials and were not received on snapd-snap.socket
type openAccess struct{}

func (ac openAccess) CheckAccess(r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	return requireSnapdSocket(ucred)
}

// authenticatedAccess allows requests from authenticated users,
// provided they were not received on snapd-snap.socket
//
// A user is considered authenticated if they provide a macaroon, are
// the root user according to peer credentials, or granted access by
// Polkit.
type authenticatedAccess struct {
	Polkit string
}

func (ac authenticatedAccess) CheckAccess(r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	if rspe := requireSnapdSocket(ucred); rspe != nil {
		return rspe
	}

	if user != nil {
		return nil
	}

	if ucred.Uid == 0 {
		return nil
	}

	// We check polkit last because it may result in the user
	// being prompted for authorisation. This should be avoided if
	// access is otherwise granted.
	if ac.Polkit != "" {
		return checkPolkitAction(r, ucred, ac.Polkit)
	}

	return Unauthorized("access denied")
}

// rootAccess allows requests from the root uid, provided they
// were not received on snapd-snap.socket
type rootAccess struct{}

func (ac rootAccess) CheckAccess(r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	if rspe := requireSnapdSocket(ucred); rspe != nil {
		return rspe
	}

	if ucred.Uid == 0 {
		return nil
	}
	return Forbidden("access denied")
}

// snapAccess allows requests from the snapd-snap.socket
type snapAccess struct{}

func (ac snapAccess) CheckAccess(r *http.Request, ucred *ucrednet, user *auth.UserState) *apiError {
	if ucred == nil {
		return Forbidden("access denied")
	}

	if ucred.Socket == dirs.SnapSocket {
		return nil
	}
	// FIXME: should snapctl access be allowed on the main socket?
	return Forbidden("access denied")
}
