// Copyright (c) 2016 Pulcy.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package service

import (
	"bytes"
	"os"
	"os/exec"
)

// checkRedisAppendOnlyFile performs a check (and fix) on any appendonly file.
func (s *Service) checkRedisAppendOnlyFile() error {
	if s.RedisAppendOnlyPath == "" {
		// Not appendonly file configured
		s.Logger.Info("No appendonly file configured, so we cannot check it")
		return nil
	}

	if _, err := os.Stat(s.RedisAppendOnlyPath); os.IsNotExist(err) {
		// No appendonly file found
		s.Logger.Infof("Appendonly file '%s' does not exist, so we cannot check it", s.RedisAppendOnlyPath)
		return nil
	}

	s.Logger.Infof("Checking appendonly file '%s'...", s.RedisAppendOnlyPath)
	args := []string{"--fix", s.RedisAppendOnlyPath}
	cmd := exec.Command("redis-check-aof", args...)
	cmd.Stdin = bytes.NewReader([]byte("y"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return maskAny(err)
	}

	return nil
}
