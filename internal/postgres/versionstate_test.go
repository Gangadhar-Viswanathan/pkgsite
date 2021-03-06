// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"golang.org/x/pkgsite/internal"
	"golang.org/x/pkgsite/internal/testing/sample"
)

func TestModuleVersionState(t *testing.T) {
	defer ResetTestDB(testDB, t)
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// verify that latest index timestamp works
	initialTime, err := testDB.LatestIndexTimestamp(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := (time.Time{}); initialTime != want {
		t.Errorf("testDB.LatestIndexTimestamp(ctx) = %v, want %v", initialTime, want)
	}

	now := sample.NowTruncated()
	latest := now.Add(10 * time.Second)
	// insert a FooVersion with no Timestamp, to ensure that it is later updated
	// on conflict.
	initialFooVersion := &internal.IndexVersion{
		Path:    "foo.com/bar",
		Version: "v1.0.0",
	}
	if err := testDB.InsertIndexVersions(ctx, []*internal.IndexVersion{initialFooVersion}); err != nil {
		t.Fatal(err)
	}
	fooVersion := &internal.IndexVersion{
		Path:      "foo.com/bar",
		Version:   "v1.0.0",
		Timestamp: now,
	}
	bazVersion := &internal.IndexVersion{
		Path:      "baz.com/quux",
		Version:   "v2.0.1",
		Timestamp: latest,
	}
	versions := []*internal.IndexVersion{fooVersion, bazVersion}
	if err := testDB.InsertIndexVersions(ctx, versions); err != nil {
		t.Fatal(err)
	}

	gotVersions, err := testDB.GetNextModulesToFetch(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}

	wantVersions := []*internal.ModuleVersionState{
		{ModulePath: "baz.com/quux", Version: "v2.0.1", IndexTimestamp: bazVersion.Timestamp},
		{ModulePath: "foo.com/bar", Version: "v1.0.0", IndexTimestamp: fooVersion.Timestamp},
	}
	ignore := cmpopts.IgnoreFields(internal.ModuleVersionState{}, "CreatedAt", "LastProcessedAt", "NextProcessedAfter")
	if diff := cmp.Diff(wantVersions, gotVersions, ignore); diff != "" {
		t.Fatalf("testDB.GetVersionsToFetch(ctx, 10) mismatch (-want +got):\n%s", diff)
	}

	var (
		statusCode      = 500
		fetchErr        = errors.New("bad request")
		goModPath       = "goModPath"
		pkgVersionState = &internal.PackageVersionState{
			ModulePath:  "foo.com/bar",
			PackagePath: "foo.com/bar/foo",
			Version:     "v1.0.0",
			Status:      500,
		}
	)
	if err := testDB.UpsertModuleVersionState(ctx, fooVersion.Path, fooVersion.Version, "", fooVersion.Timestamp, statusCode, goModPath, fetchErr, []*internal.PackageVersionState{pkgVersionState}); err != nil {
		t.Fatal(err)
	}
	errString := fetchErr.Error()
	numPackages := 1
	wantFooState := &internal.ModuleVersionState{
		ModulePath:     "foo.com/bar",
		Version:        "v1.0.0",
		IndexTimestamp: now,
		TryCount:       1,
		GoModPath:      goModPath,
		Error:          errString,
		Status:         statusCode,
		NumPackages:    &numPackages,
	}
	gotFooState, err := testDB.GetModuleVersionState(ctx, wantFooState.ModulePath, wantFooState.Version)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(wantFooState, gotFooState, ignore); diff != "" {
		t.Errorf("testDB.GetModuleVersionState(ctx, %q, %q) mismatch (-want +got)\n%s", wantFooState.ModulePath, wantFooState.Version, diff)
	}

	gotPVS, err := testDB.GetPackageVersionState(ctx, pkgVersionState.PackagePath, pkgVersionState.ModulePath, pkgVersionState.Version)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(pkgVersionState, gotPVS); diff != "" {
		t.Errorf("testDB.GetPackageVersionStates(ctx, %q, %q) mismatch (-want +got)\n%s", wantFooState.ModulePath, wantFooState.Version, diff)
	}

	gotPkgVersionStates, err := testDB.GetPackageVersionStatesForModule(ctx,
		wantFooState.ModulePath, wantFooState.Version)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff([]*internal.PackageVersionState{pkgVersionState}, gotPkgVersionStates); diff != "" {
		t.Errorf("testDB.GetPackageVersionStates(ctx, %q, %q) mismatch (-want +got)\n%s", wantFooState.ModulePath, wantFooState.Version, diff)
	}

	stats, err := testDB.GetVersionStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	wantStats := &VersionStats{
		LatestTimestamp: latest,
		VersionCounts: map[int]int{
			0:   1,
			500: 1,
		},
	}
	if diff := cmp.Diff(wantStats, stats); diff != "" {
		t.Errorf("testDB.GetVersionStats(ctx) mismatch (-want +got):\n%s", diff)
	}

	if _, err := testDB.GetRecentFailedVersions(ctx, 10); err != nil {
		t.Fatal(err)
	}
}
