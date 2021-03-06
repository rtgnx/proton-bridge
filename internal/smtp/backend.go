// Copyright (c) 2020 Proton Technologies AG
//
// This file is part of ProtonMail Bridge.
//
// ProtonMail Bridge is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// ProtonMail Bridge is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with ProtonMail Bridge.  If not, see <https://www.gnu.org/licenses/>.

package smtp

import (
	"strings"
	"time"

	"github.com/ProtonMail/proton-bridge/internal/bridge"
	"github.com/ProtonMail/proton-bridge/internal/preferences"
	"github.com/ProtonMail/proton-bridge/pkg/config"
	"github.com/ProtonMail/proton-bridge/pkg/listener"
	goSMTPBackend "github.com/emersion/go-smtp"
)

type panicHandler interface {
	HandlePanic()
}

type smtpBackend struct {
	panicHandler            panicHandler
	eventListener           listener.Listener
	preferences             *config.Preferences
	bridge                  bridger
	shouldSendNoEncChannels map[string]chan bool
	sendRecorder            *sendRecorder
}

// NewSMTPBackend returns struct implementing go-smtp/backend interface.
func NewSMTPBackend(
	panicHandler panicHandler,
	eventListener listener.Listener,
	preferences *config.Preferences,
	bridge *bridge.Bridge,
) *smtpBackend { //nolint[golint]
	return newSMTPBackend(panicHandler, eventListener, preferences, newBridgeWrap(bridge))
}

func newSMTPBackend(
	panicHandler panicHandler,
	eventListener listener.Listener,
	preferences *config.Preferences,
	bridge bridger,
) *smtpBackend {
	return &smtpBackend{
		panicHandler:            panicHandler,
		eventListener:           eventListener,
		preferences:             preferences,
		bridge:                  bridge,
		shouldSendNoEncChannels: make(map[string]chan bool),
		sendRecorder:            newSendRecorder(),
	}
}

// Login authenticates a user.
func (sb *smtpBackend) Login(username, password string) (goSMTPBackend.User, error) {
	// Called from go-smtp in goroutines - we need to handle panics for each function.
	defer sb.panicHandler.HandlePanic()
	username = strings.ToLower(username)

	user, err := sb.bridge.GetUser(username)
	if err != nil {
		log.Warn("Cannot get user: ", err)
		return nil, err
	}
	if err := user.CheckBridgeLogin(password); err != nil {
		log.WithError(err).Error("Could not check bridge password")
		// Apple Mail sometimes generates a lot of requests very quickly. It's good practice
		// to have a timeout after bad logins so that we can slow those requests down a little bit.
		time.Sleep(10 * time.Second)
		return nil, err
	}
	// Client can log in only using address so we can properly close all SMTP connections.
	addressID, err := user.GetAddressID(username)
	if err != nil {
		log.Error("Cannot get addressID: ", err)
		return nil, err
	}
	// AddressID is only for split mode--it has to be empty for combined mode.
	if user.IsCombinedAddressMode() {
		addressID = ""
	}
	return newSMTPUser(sb.panicHandler, sb.eventListener, sb, user, addressID)
}

func (sb *smtpBackend) shouldReportOutgoingNoEnc() bool {
	return sb.preferences.GetBool(preferences.ReportOutgoingNoEncKey)
}

func (sb *smtpBackend) ConfirmNoEncryption(messageID string, shouldSend bool) {
	if ch, ok := sb.shouldSendNoEncChannels[messageID]; ok {
		ch <- shouldSend
	}
}
