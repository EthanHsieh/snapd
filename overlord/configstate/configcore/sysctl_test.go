// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package configcore_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type sysctlSuite struct {
	configcoreSuite
	testutil.BaseTest
	mockSysctlPath        string
	mockSysctlDefConfPath string
	mockSysctl            *testutil.MockCmd
}

var _ = Suite(&sysctlSuite{})

func (s *sysctlSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.configcoreSuite.SetUpTest(c)
	s.BaseTest.AddCleanup(release.MockOnClassic(false))

	s.mockSysctl = testutil.MockCommand(c, filepath.Join(dirs.GlobalRootDir, "/lib/systemd/systemd-sysctl"), "")
	s.BaseTest.AddCleanup(func() { s.mockSysctl.Restore() })

	s.mockSysctlPath = filepath.Join(dirs.GlobalRootDir, "/etc/sysctl.d/99-snapd.conf")
	c.Assert(os.MkdirAll(filepath.Dir(s.mockSysctlPath), 0755), IsNil)
	s.mockSysctlDefConfPath = filepath.Join(dirs.GlobalRootDir, "/etc/sysctl.d/10-console-messages.conf")
}

func (s *sysctlSuite) TearDownTest(c *C) {
	s.configcoreSuite.TearDownTest(c)
	s.BaseTest.TearDownTest(c)
}

func (s *sysctlSuite) makeMockDefConf(c *C) {
	err := ioutil.WriteFile(s.mockSysctlDefConfPath, []byte(`
kernel.printk = 4 4 1 7
`), 0644)
	c.Assert(err, IsNil)
}

func (s *sysctlSuite) TestConfigureSysctlIntegration(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.kernel.printk.console-loglevel": "2",
		},
	})
	c.Assert(err, IsNil)
	c.Check(s.mockSysctlPath, testutil.FileEquals, "kernel.printk = 2 4 1 7\n")
	c.Check(s.mockSysctl.Calls(), DeepEquals, [][]string{
		{"systemd-sysctl", "--prefix=kernel.printk"},
	})
	s.mockSysctl.ForgetCalls()

	// Unset console-loglevel and restore default vaule
	s.makeMockDefConf(c)
	err = configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.kernel.printk.console-loglevel": "",
		},
	})
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(s.mockSysctlPath), Equals, false)
	c.Check(osutil.FileExists(s.mockSysctlDefConfPath), Equals, true)
	c.Check(s.mockSysctl.Calls(), DeepEquals, [][]string{
		{"systemd-sysctl", "--prefix=kernel.printk"},
	})
}

func (s *sysctlSuite) TestConfigureLoglevelUnderRange(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.kernel.printk.console-loglevel": "-1",
		},
	})
	c.Check(osutil.FileExists(s.mockSysctlPath), Equals, false)
	c.Assert(err, ErrorMatches, `console-loglevel must be a number between 0 and 7, not "-1"`)
}

func (s *sysctlSuite) TestConfigureLoglevelOverRange(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.kernel.printk.console-loglevel": "8",
		},
	})
	c.Check(osutil.FileExists(s.mockSysctlPath), Equals, false)
	c.Assert(err, ErrorMatches, `console-loglevel must be a number between 0 and 7, not "8"`)
}

func (s *sysctlSuite) TestConfigureLoglevelRejected(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.kernel.printk.console-loglevel": "invalid",
		},
	})
	c.Check(osutil.FileExists(s.mockSysctlPath), Equals, false)
	c.Assert(err, ErrorMatches, `console-loglevel must be a number between 0 and 7, not "invalid"`)
}

func (s *sysctlSuite) TestConfigureLoglevelNoDefConf(c *C) {
	err := configcore.Run(&mockConf{
		state: s.state,
		conf: map[string]interface{}{
			"system.kernel.printk.console-loglevel": "",
		},
	})
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(s.mockSysctlDefConfPath), Equals, false)
}

func (s *sysctlSuite) TestConfigureSysctlIntegrationNoSetting(c *C) {
	s.makeMockDefConf(c)
	err := configcore.Run(&mockConf{
		state: s.state,
		conf:  map[string]interface{}{},
	})
	c.Assert(err, IsNil)
	c.Check(osutil.FileExists(s.mockSysctlPath), Equals, false)
	c.Check(osutil.FileExists(s.mockSysctlDefConfPath), Equals, true)
}

func (s *sysctlSuite) TestFilesystemOnlyApply(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"system.kernel.printk.console-loglevel": "4",
	})

	tmpDir := c.MkDir()
	c.Assert(os.MkdirAll(filepath.Join(tmpDir, "/etc/sysctl.d/"), 0755), IsNil)
	c.Assert(configcore.FilesystemOnlyApply(tmpDir, conf, nil), IsNil)

	networkSysctlPath := filepath.Join(tmpDir, "/etc/sysctl.d/99-snapd.conf")
	c.Check(networkSysctlPath, testutil.FileEquals, "kernel.printk = 4 4 1 7\n")

	// sysctl was not executed
	c.Check(s.mockSysctl.Calls(), HasLen, 0)
}
