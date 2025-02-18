// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package archiver_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/siderolabs/talos/pkg/archiver"
)

type WalkerSuite struct {
	CommonSuite
}

func (suite *WalkerSuite) TestIterationDir() {
	ch, err := archiver.Walker(context.Background(), suite.tmpDir, archiver.WithSkipRoot())
	suite.Require().NoError(err)

	relPaths := []string(nil)

	for fi := range ch {
		suite.Require().NoError(fi.Error)
		relPaths = append(relPaths, fi.RelPath)

		if fi.RelPath == "usr/bin/mv" {
			suite.Assert().Equal("/usr/bin/cp", fi.Link)
		}
	}

	suite.Assert().Equal([]string{
		"dev", "dev/random",
		"etc", "etc/certs", "etc/certs/ca.crt", "etc/hostname",
		"lib", "lib/dynalib.so",
		"usr", "usr/bin", "usr/bin/cp", "usr/bin/mv",
	},
		relPaths)
}

func (suite *WalkerSuite) TestIterationFilter() {
	ch, err := archiver.Walker(context.Background(), suite.tmpDir, archiver.WithSkipRoot(), archiver.WithFnmatchPatterns("dev/*", "lib"))
	suite.Require().NoError(err)

	relPaths := []string(nil)

	for fi := range ch {
		suite.Require().NoError(fi.Error)
		relPaths = append(relPaths, fi.RelPath)

		if fi.RelPath == "usr/bin/mv" {
			suite.Assert().Equal("/usr/bin/cp", fi.Link)
		}
	}

	suite.Assert().Equal([]string{
		"dev/random",
		"lib",
	},
		relPaths)
}

func (suite *WalkerSuite) TestIterationMaxRecurseDepth() {
	for _, test := range []struct {
		maxDepth int
		result   []string
	}{
		{
			maxDepth: -1,
			result:   []string{".", "dev", "dev/random", "etc", "etc/certs", "etc/certs/ca.crt", "etc/hostname", "lib", "lib/dynalib.so", "usr", "usr/bin", "usr/bin/cp", "usr/bin/mv"},
		},
		{
			// confusing case
			maxDepth: 0,
			result:   []string{".", "dev", "etc", "lib", "usr"},
		},
		{
			maxDepth: 1,
			result:   []string{".", "dev", "etc", "lib", "usr"},
		},
		{
			maxDepth: 2,
			result:   []string{".", "dev", "dev/random", "etc", "etc/certs", "etc/hostname", "lib", "lib/dynalib.so", "usr", "usr/bin"},
		},
		{
			maxDepth: 3,
			result:   []string{".", "dev", "dev/random", "etc", "etc/certs", "etc/certs/ca.crt", "etc/hostname", "lib", "lib/dynalib.so", "usr", "usr/bin", "usr/bin/cp", "usr/bin/mv"},
		},
		{
			maxDepth: 4,
			result:   []string{".", "dev", "dev/random", "etc", "etc/certs", "etc/certs/ca.crt", "etc/hostname", "lib", "lib/dynalib.so", "usr", "usr/bin", "usr/bin/cp", "usr/bin/mv"},
		},
	} {
		test := test
		suite.Run(strconv.Itoa(test.maxDepth), func() {
			suite.T().Parallel()

			ch, err := archiver.Walker(context.Background(), suite.tmpDir, archiver.WithMaxRecurseDepth(test.maxDepth))
			suite.Require().NoError(err)

			var result []string

			for fi := range ch {
				suite.Require().NoError(fi.Error)
				result = append(result, fi.RelPath)
			}

			suite.Equal(test.result, result)
		})
	}
}

func (suite *WalkerSuite) TestIterationFile() {
	ch, err := archiver.Walker(context.Background(), filepath.Join(suite.tmpDir, "usr/bin/cp"))
	suite.Require().NoError(err)

	relPaths := []string(nil)

	for fi := range ch {
		suite.Require().NoError(fi.Error)
		relPaths = append(relPaths, fi.RelPath)
	}

	suite.Assert().Equal([]string{"cp"},
		relPaths)
}

func (suite *WalkerSuite) TestIterationSymlink() {
	original := filepath.Join(suite.tmpDir, "original")
	err := os.Mkdir(original, 0o755)
	suite.Require().NoError(err)

	defer func() {
		err = os.RemoveAll(original)
		suite.Require().NoError(err)
	}()

	// NB: We make this a relative symlink to make the test more complete.
	newname := filepath.Join(suite.tmpDir, "new")
	err = os.Symlink("original", newname)
	suite.Require().NoError(err)

	defer func() {
		err = os.Remove(newname)
		suite.Require().NoError(err)
	}()

	err = os.WriteFile(filepath.Join(original, "original.txt"), []byte{}, 0o666)
	suite.Require().NoError(err)

	ch, err := archiver.Walker(context.Background(), newname)
	suite.Require().NoError(err)

	relPaths := []string(nil)

	for fi := range ch {
		suite.Require().NoError(fi.Error)
		relPaths = append(relPaths, fi.RelPath)
	}

	suite.Assert().Equal([]string{".", "original.txt"}, relPaths)
}

func (suite *WalkerSuite) TestIterationNotFound() {
	_, err := archiver.Walker(context.Background(), filepath.Join(suite.tmpDir, "doesntlivehere"))
	suite.Require().Error(err)
}

func (suite *WalkerSuite) TestIterationTypes() {
	ch, err := archiver.Walker(context.Background(), suite.tmpDir, archiver.WithFileTypes(archiver.DirectoryFileType))
	suite.Require().NoError(err)

	relPaths := []string(nil)

	for fi := range ch {
		suite.Require().NoError(fi.Error)
		relPaths = append(relPaths, fi.RelPath)
	}

	suite.Assert().Equal([]string{
		".", "dev", "etc", "etc/certs", "lib", "usr", "usr/bin",
	},
		relPaths)
}

func TestWalkerSuite(t *testing.T) {
	suite.Run(t, new(WalkerSuite))
}
