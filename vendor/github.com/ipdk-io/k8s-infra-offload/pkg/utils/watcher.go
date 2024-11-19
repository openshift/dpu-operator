// Copyright (c) 2022 Intel Corporation.  All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License")
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

package utils

import (
	"fmt"
	"time"
)

type watcher interface {
	handleEvents()
	initialCheck() bool
	getChannels() (chan bool, chan bool, chan error)
	getTimeout() time.Duration
	addWatchedResources() error
	close() error
}

// WaitFor will wait for watcher result
func WaitFor(w watcher) error {
	defer w.close()
	// check if resource is already avialable
	if w.initialCheck() {
		return nil
	}

	go w.handleEvents()

	if err := w.addWatchedResources(); err != nil {
		_, quit, errCh := w.getChannels()
		quit <- true
		if watcherErr := <-errCh; watcherErr != nil {
			return fmt.Errorf("%s: %s", watcherErr, err)
		}
		return err
	}

	return processEvents(w)
}

func processEvents(w watcher) error {
	done, quit, errCh := w.getChannels()
	timeout := w.getTimeout()
	var result bool
	if timeout > 0 {
		// wait until timeout
		select {
		case <-time.After(timeout):
			quit <- true
			return fmt.Errorf("timeout while waiting for the resource")
		case result = <-done:
			if !result {
				return fmt.Errorf("error while waiting for the resource")
			}
			return nil
		}
	} else {
		// wait forever
		result = <-done
		if !result {
			return fmt.Errorf("error while waiting for the resource")
		}
	}
	err := <-errCh
	return err
}
