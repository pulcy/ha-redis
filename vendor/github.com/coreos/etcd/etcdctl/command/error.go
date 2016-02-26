// Copyright 2015 CoreOS, Inc.
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

package command

import (
	"fmt"
	"os"

	"github.com/coreos/etcd/client"
)

const (
	ExitSuccess = iota
	ExitBadArgs
	ExitBadConnection
	ExitBadAuth
	ExitServerError
	ExitClusterNotHealthy
)

func handleError(code int, err error) {
	fmt.Fprintln(os.Stderr, "Error: ", err)
	if cerr, ok := err.(*client.ClusterError); ok {
		fmt.Fprintln(os.Stderr, cerr.Detail())
	}
	os.Exit(code)
}
