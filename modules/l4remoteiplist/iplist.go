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

package l4remoteiplist

import (
	"bufio"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sync"

	"github.com/caddyserver/caddy/v2"
	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"
)

type IPList struct {
	ipFile            string         // File containing all IPs to be matched, gets continously monitored
	ipAddresses       []netip.Addr   // List of currently loaded IP addresses that would matched
	ctx               *caddy.Context // Caddy context, used to detect when to shut down
	logger            *zap.Logger
	reloadNeededMutex sync.Mutex // Mutex to ensure proper concurrent handling of reloads
	reloadNeeded      bool       // Flag indicating whether a reload of the IPs is needed
}

// Creates a new IPList, creating the ipFile if it is not present
func NewIPList(ipFile string, ctx *caddy.Context, logger *zap.Logger) (*IPList, error) {
	ipList := &IPList{
		ipFile:       ipFile,
		ctx:          ctx,
		logger:       logger,
		reloadNeeded: true,
	}

	// make sure the directory containing the ipFile exists
	// otherwise, the fsnotify watcher will not work
	if !ipList.ipFileDirectoryExists() {
		return nil, fmt.Errorf("could not find the directory containing the IP file to monitor: %v", ipFile)
	}

	return ipList, nil
}

// Check whether a IP address is currently contained in the IP list
func (b *IPList) IsMatched(ip netip.Addr) bool {
	// First reload the IP list if needed to ensure IPs are always up to date
	b.reloadNeededMutex.Lock()
	if b.reloadNeeded {
		err := b.loadIPAddresses()
		if err != nil {
			b.logger.Error("could not load IP addresses", zap.Error(err))
		} else {
			b.reloadNeeded = false
			b.logger.Debug("reloaded IP addresses")
		}
	}
	b.reloadNeededMutex.Unlock()

	for _, listIP := range b.ipAddresses {
		if listIP.Compare(ip) == 0 {
			return true
		}
	}
	return false
}

// Start to monitor the IP list
func (b *IPList) StartMonitoring() {
	go b.monitor()
}

func (b *IPList) ipFileDirectoryExists() bool {
	// Make sure the directory containing the IP list exists
	dirpath := filepath.Dir(b.ipFile)
	st, err := os.Lstat(dirpath)
	if err != nil || !st.IsDir() {
		return false
	}
	return true
}

func (b *IPList) ipFileExists() bool {
	// Make sure the IP list exists and is a file
	st, err := os.Lstat(b.ipFile)
	if err != nil || st.IsDir() {
		return false
	}
	return true
}

func (b *IPList) monitor() {
	// Create a new watcher
	w, err := fsnotify.NewWatcher()
	if err != nil {
		b.logger.Error("error creating a new filesystem watcher", zap.Error(err))
		return
	}
	defer w.Close()

	if !b.ipFileDirectoryExists() {
		b.logger.Error("directory containing the IP file to monitor does not exist")
		return
	}

	// Monitor the directory of the file
	err = w.Add(filepath.Dir(b.ipFile))
	if err != nil {
		b.logger.Error("error watching the file", zap.Error(err))
		return
	}

	for {
		select {
		case <-b.ctx.Done():
			// Check if Caddy closed the context
			b.logger.Debug("caddy closed the context")
			return
		case err, ok := <-w.Errors:
			b.logger.Error("error from file watcher", zap.Error(err))
			if !ok {
				b.logger.Error("file watcher was closed")
				return
			}
		case e, ok := <-w.Events:
			if !ok {
				b.logger.Error("file watcher was closed")
				return
			}

			// Check if the IP list has changed
			if b.ipFile == e.Name && (e.Has(fsnotify.Create) || e.Has(fsnotify.Write)) {
				b.reloadNeededMutex.Lock()
				b.reloadNeeded = true
				b.reloadNeededMutex.Unlock()
			}
		}
	}
}

// Loads the IP addresses from the IP list
func (b *IPList) loadIPAddresses() error {
	if !b.ipFileExists() {
		b.logger.Debug("ip file not found, nothing to monitor")
		b.ipAddresses = make([]netip.Addr, 0)
		return nil
	}

	file, err := os.Open(b.ipFile)
	if err != nil {
		return fmt.Errorf("error opening the IP list file %v: %w", b.ipFile, err)
	}
	defer file.Close()

	var ipAddresses []netip.Addr
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		ip, err := netip.ParseAddr(line)
		if err == nil {
			// only append valid IP addresses (ignore lines that
			// have not been parsed to an IP address, e.g. comments)
			ipAddresses = append(ipAddresses, ip)
		}
	}
	err = scanner.Err()
	if err != nil {
		return fmt.Errorf("error reading the IPs from %v: %w", b.ipFile, err)
	}

	b.ipAddresses = ipAddresses
	return nil
}
