// Copyright 2015, Google Inc. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package endtoend

import (
	"fmt"
	"strings"
	"testing"

	"github.com/youtube/vitess/go/vt/schema"
	"github.com/youtube/vitess/go/vt/tabletserver/endtoend/framework"
)

func TestUncacheableTables(t *testing.T) {
	client := framework.NewDefaultClient()

	nocacheTables := []struct {
		name   string
		create string
		drop   string
	}{{
		create: "create table vtocc_nocache(eid int, primary key (eid)) comment 'vtocc_nocache'",
		drop:   "drop table vtocc_nocache",
	}, {
		create: "create table vtocc_nocache(somecol int)",
		drop:   "drop table vtocc_nocache",
	}, {
		create: "create table vtocc_nocache(charcol varchar(10), primary key(charcol))",
		drop:   "drop table vtocc_nocache",
	}}
	for _, tcase := range nocacheTables {
		_, err := client.Execute(tcase.create, nil)
		if err != nil {
			t.Error(err)
			return
		}
		table, ok := framework.DebugSchema()["vtocc_nocache"]
		client.Execute(tcase.drop, nil)
		if !ok {
			t.Errorf("%s: table vtocc_nocache not found in schema", tcase.create)
			continue
		}
		if table.CacheType != schema.CACHE_NONE {
			t.Errorf("CacheType: %d, want %d", table.CacheType, schema.CACHE_NONE)
		}
	}
}

func TestOverrideTables(t *testing.T) {
	testCases := []struct {
		table     string
		cacheType int
	}{{
		table:     "vtocc_cached2",
		cacheType: schema.CACHE_RW,
	}, {
		table:     "vtocc_view",
		cacheType: schema.CACHE_RW,
	}, {
		table:     "vtocc_part1",
		cacheType: schema.CACHE_W,
	}, {
		table:     "vtocc_part2",
		cacheType: schema.CACHE_W,
	}}
	for _, tcase := range testCases {
		table, ok := framework.DebugSchema()[tcase.table]
		if !ok {
			t.Errorf("Table %s not found in schema", tcase.table)
			return
		}
		if table.CacheType != tcase.cacheType {
			t.Errorf("CacheType: %d, want %d", table.CacheType, tcase.cacheType)
		}
	}
}

func TestCacheDisallows(t *testing.T) {
	client := framework.NewDefaultClient()
	testCases := []struct {
		query string
		bv    map[string]interface{}
		err   string
	}{{
		query: "select bid, eid from vtocc_cached2 where eid = 1 and bid = 1",
		err:   "error: type mismatch",
	}, {
		query: "select * from vtocc_cached2 where eid = 2 and bid = 'foo' limit :a",
		bv:    map[string]interface{}{"a": -1},
		err:   "error: negative limit",
	}}
	for _, tcase := range testCases {
		_, err := client.Execute(tcase.query, tcase.bv)
		if err == nil || !strings.HasPrefix(err.Error(), tcase.err) {
			t.Errorf("Error: %v, want %s", err, tcase.err)
			return
		}
	}
}

func TestCacheListArgs(t *testing.T) {
	client := framework.NewDefaultClient()
	query := "select * from vtocc_cached1 where eid in ::list"
	successCases := []struct {
		bv       map[string]interface{}
		rowcount uint64
	}{{
		bv:       map[string]interface{}{"list": []interface{}{3, 4, 32768}},
		rowcount: 2,
	}, {
		bv:       map[string]interface{}{"list": []interface{}{3, 4}},
		rowcount: 2,
	}, {
		bv:       map[string]interface{}{"list": []interface{}{3}},
		rowcount: 1,
	}}
	for _, success := range successCases {
		qr, err := client.Execute(query, success.bv)
		if err != nil {
			t.Error(err)
			continue
		}
		if qr.RowsAffected != success.rowcount {
			t.Errorf("RowsAffected: %d, want %d", qr.RowsAffected, success.rowcount)
		}
	}

	_, err := client.Execute(query, map[string]interface{}{"list": []interface{}{}})
	want := "error: empty list supplied"
	if err == nil || !strings.HasPrefix(err.Error(), want) {
		t.Errorf("Error: %v, want %s", err, want)
		return
	}
}

func verifyVtoccCached2(t *testing.T, table string) error {
	client := framework.NewDefaultClient()
	query := fmt.Sprintf("select * from %s where eid = 2 and bid = 'foo'", table)
	_, err := client.Execute(query, nil)
	if err != nil {
		return err
	}
	tstart := framework.TableStats()[table]
	_, err = client.Execute(query, nil)
	if err != nil {
		return err
	}
	tend := framework.TableStats()[table]
	if tend.Hits != tstart.Hits+1 {
		return fmt.Errorf("Hits: %d, want %d", tend.Hits, tstart.Hits+1)
	}
	return nil
}

func TestUncache(t *testing.T) {
	// Verify rowcache is working vtocc_cached2
	err := verifyVtoccCached2(t, "vtocc_cached2")
	if err != nil {
		t.Error(err)
		return
	}

	// Disable rowcache for vtocc_cached2
	client := framework.NewDefaultClient()
	_, err = client.Execute("alter table vtocc_cached2 comment 'vtocc_nocache'", nil)
	if err != nil {
		t.Error(err)
		return
	}
	_, err = client.Execute("select * from vtocc_cached2 where eid = 2 and bid = 'foo'", nil)
	if err != nil {
		t.Error(err)
	}
	if tstat, ok := framework.TableStats()["vtocc_cached2"]; ok {
		t.Errorf("table stats was found: %v, want not found", tstat)
	}

	// Re-enable rowcache and verify it's working
	_, err = client.Execute("alter table vtocc_cached2 comment ''", nil)
	if err != nil {
		t.Error(err)
		return
	}
	err = verifyVtoccCached2(t, "vtocc_cached2")
	if err != nil {
		t.Error(err)
		return
	}
}

func TestRename(t *testing.T) {
	// Verify rowcache is working vtocc_cached2
	err := verifyVtoccCached2(t, "vtocc_cached2")
	if err != nil {
		t.Error(err)
		return
	}

	// Rename & test
	client := framework.NewDefaultClient()
	_, err = client.Execute("alter table vtocc_cached2 rename to vtocc_renamed", nil)
	if err != nil {
		t.Error(err)
		return
	}
	if tstat, ok := framework.TableStats()["vtocc_cached2"]; ok {
		t.Errorf("table stats was found: %v, want not found", tstat)
	}

	err = verifyVtoccCached2(t, "vtocc_renamed")
	if err != nil {
		t.Error(err)
		return
	}

	// Rename back & verify
	_, err = client.Execute("rename table vtocc_renamed to vtocc_cached2", nil)
	if err != nil {
		t.Error(err)
		return
	}
	err = verifyVtoccCached2(t, "vtocc_cached2")
	if err != nil {
		t.Error(err)
		return
	}
}

func TestSpotCheck(t *testing.T) {
	vstart := framework.DebugVars()
	client := framework.NewDefaultClient()
	_, err := client.Execute("select * from vtocc_cached2 where eid = 2 and bid = 'foo'", nil)
	if err != nil {
		t.Error(err)
		return
	}
	if err := compareIntDiff(framework.DebugVars(), "RowcacheSpotCheckCount", vstart, 0); err != nil {
		t.Error(err)
	}

	defer framework.DefaultServer.SetSpotCheckRatio(framework.DefaultServer.SpotCheckRatio())
	framework.DefaultServer.SetSpotCheckRatio(1)
	if err := verifyIntValue(framework.DebugVars(), "RowcacheSpotCheckRatio", 1); err != nil {
		t.Error(err)
	}

	vstart = framework.DebugVars()
	_, err = client.Execute("select * from vtocc_cached2 where eid = 2 and bid = 'foo'", nil)
	if err != nil {
		t.Error(err)
		return
	}
	if err := compareIntDiff(framework.DebugVars(), "RowcacheSpotCheckCount", vstart, 1); err != nil {
		t.Error(err)
	}

	vstart = framework.DebugVars()
	_, err = client.Execute("select * from vtocc_cached1 where eid in (9)", nil)
	if err != nil {
		t.Error(err)
		return
	}
	if err := compareIntDiff(framework.DebugVars(), "RowcacheSpotCheckCount", vstart, 0); err != nil {
		t.Error(err)
	}
	_, err = client.Execute("select * from vtocc_cached1 where eid in (9)", nil)
	if err != nil {
		t.Error(err)
		return
	}
	if err := compareIntDiff(framework.DebugVars(), "RowcacheSpotCheckCount", vstart, 1); err != nil {
		t.Error(err)
	}
}

func TestCacheTypes(t *testing.T) {
	client := framework.NewDefaultClient()
	badRequests := []struct {
		query string
		bv    map[string]interface{}
	}{{
		query: "select * from vtocc_cached2 where eid = 'str' and bid = 'str'",
	}, {
		query: "select * from vtocc_cached2 where eid = :str and bid = :str",
		bv:    map[string]interface{}{"str": "str"},
	}, {
		query: "select * from vtocc_cached2 where eid = 1 and bid = 1",
	}, {
		query: "select * from vtocc_cached2 where eid = :id and bid = :id",
		bv:    map[string]interface{}{"id": 1},
	}, {
		query: "select * from vtocc_cached2 where eid = 1.2 and bid = 1.2",
	}, {
		query: "select * from vtocc_cached2 where eid = :fl and bid = :fl",
		bv:    map[string]interface{}{"fl": 1.2},
	}}
	want := "error: type mismatch"
	for _, request := range badRequests {
		_, err := client.Execute(request.query, request.bv)
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("Error: %v, want %s", err, want)
		}
	}
}

func TestNoData(t *testing.T) {
	qr, err := framework.NewDefaultClient().Execute("select * from vtocc_cached2 where eid = 6 and name = 'bar'", nil)
	if err != nil {
		t.Error(err)
		return
	}
	if qr.RowsAffected != 0 {
		t.Errorf("RowsAffected: %d, want 0", qr.RowsAffected)
	}
}

func TestCacheStats(t *testing.T) {
	client := framework.NewDefaultClient()
	query := "select * from vtocc_cached2 where eid = 2 and bid = 'foo'"
	_, err := client.Execute(query, nil)
	if err != nil {
		t.Error(err)
		return
	}
	vstart := framework.DebugVars()
	_, err = client.Execute(query, nil)
	if err != nil {
		t.Error(err)
		return
	}
	if err := compareIntDiff(framework.DebugVars(), "RowcacheStats/vtocc_cached2.Hits", vstart, 1); err != nil {
		t.Error(err)
	}

	vstart = framework.DebugVars()
	_, err = client.Execute("update vtocc_part2 set data2 = 2 where key3 = 1", nil)
	if err != nil {
		t.Error(err)
		return
	}
	_, err = client.Execute("select * from vtocc_view where key2 = 1", nil)
	if err != nil {
		t.Error(err)
		return
	}
	if err := compareIntDiff(framework.DebugVars(), "RowcacheStats/vtocc_view.Misses", vstart, 1); err != nil {
		t.Error(err)
	}
}
