// Copyright (c) 2024 SICK AG
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package l4fail2ban

import (
	"fmt"
	"net"
	"net/netip"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"

	"github.com/mholt/caddy-l4/layer4"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(&Fail2Ban{})
}

type Fail2Ban struct {
	BanFile string `json:"ban_file"`

	logger  *zap.Logger
	banList *BanList
}

// CaddyModule returns the Caddy module information.
func (*Fail2Ban) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "layer4.matchers.fail2ban",
		New: func() caddy.Module { return new(Fail2Ban) },
	}
}

// Provision implements caddy.Provisioner.
func (m *Fail2Ban) Provision(ctx caddy.Context) error {
	m.logger = ctx.Logger()

	// Create new banlist, similar as in http.matchers.fail2ban
	// (https://github.com/Javex/caddy-fail2ban)
	banList, err := NewBanList(m.BanFile, &ctx, m.logger)
	if err != nil {
		// Error creating the banlist
		m.logger.Error("Error creating a new banlist", zap.Error(err))
		return err
	}
	m.banList = banList
	m.banList.StartMonitoring()
	return nil
}

// The Match will return true if the remote IP is found in the ban list
func (m *Fail2Ban) Match(cx *layer4.Connection) (bool, error) {
	clientIP, err := m.getRemoteIP(cx)
	if err != nil {
		// Error, tread IP as banned
		m.logger.Error("Error parsing the remote IP from the connection", zap.Error(err))
		return true, err
	}

	strClientIP := clientIP.String()
	if m.banList.IsBanned(strClientIP) {
		// IP is banned
		m.logger.Info("Banned IP found", zap.String("RemoteAddr", strClientIP))
		return true, nil
	}

	// IP not found in banlist, everything ok
	m.logger.Debug("Received request", zap.String("RemoteAddr", strClientIP))
	return false, nil
}

// Returns the remote IP address for a given layer4 connection.
// Same method as in layer4.MatchRemoteIP.getRemoteIP
func (m *Fail2Ban) getRemoteIP(cx *layer4.Connection) (netip.Addr, error) {
	remote := cx.Conn.RemoteAddr().String()

	ipStr, _, err := net.SplitHostPort(remote)
	if err != nil {
		ipStr = remote
	}

	ip, err := netip.ParseAddr(ipStr)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("invalid remote IP address: %s", ipStr)
	}
	return ip, nil
}

// UnmarshalCaddyfile sets up the banfile_path from Caddyfile. Syntax:
//
// fail2ban <banfile_path>
func (m *Fail2Ban) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	_, wrapper := d.Next(), d.Val() // consume wrapper name

	// Only one same-line argument is supported
	if d.CountRemainingArgs() > 1 {
		return d.ArgErr()
	}

	if d.NextArg() {
		m.BanFile = d.Val()
	}

	// No blocks are supported
	if d.NextBlock(d.Nesting()) {
		return d.Errf("malformed %s option: blocks are not supported", wrapper)
	}

	return nil
}

// Interface guards
var (
	_ layer4.ConnMatcher    = (*Fail2Ban)(nil)
	_ caddy.Provisioner     = (*Fail2Ban)(nil)
	_ caddyfile.Unmarshaler = (*Fail2Ban)(nil)
)
