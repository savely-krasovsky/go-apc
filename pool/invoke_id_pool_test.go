/*
Copyright 2019 The Vitess Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pool

import (
	"reflect"
	"strings"
	"testing"
)

func (pool *InvokeIDPool) want(want *InvokeIDPool, t *testing.T) {
	if pool.maxUsed != want.maxUsed {
		t.Errorf("pool.maxUsed = %#v, want %#v", pool.maxUsed, want.maxUsed)
	}

	if !reflect.DeepEqual(pool.used, want.used) {
		t.Errorf("pool.used = %#v, want %#v", pool.used, want.used)
	}
}

func TestInvokeIDPool_FirstGet(t *testing.T) {
	pool := NewInvokeIDPool()

	if got := pool.Get(); got != 1 {
		t.Errorf("pool.Get() = %v, want 1", got)
	}

	pool.want(&InvokeIDPool{used: map[uint32]bool{}, maxUsed: 1}, t)
}

func TestInvokeIDPool_SecondGet(t *testing.T) {
	pool := NewInvokeIDPool()
	pool.Get()

	if got := pool.Get(); got != 2 {
		t.Errorf("pool.Get() = %v, want 2", got)
	}

	pool.want(&InvokeIDPool{used: map[uint32]bool{}, maxUsed: 2}, t)
}

func TestInvokeIDPool_ReleaseToUsedSet(t *testing.T) {
	pool := NewInvokeIDPool()
	id1 := pool.Get()
	pool.Get()
	pool.Release(id1)

	pool.want(&InvokeIDPool{used: map[uint32]bool{1: true}, maxUsed: 2}, t)
}

func TestInvokeIDPool_ReleaseMaxUsed1(t *testing.T) {
	pool := NewInvokeIDPool()
	id1 := pool.Get()
	pool.Release(id1)

	pool.want(&InvokeIDPool{used: map[uint32]bool{}, maxUsed: 0}, t)
}

func TestInvokeIDPool_ReleaseMaxUsed2(t *testing.T) {
	pool := NewInvokeIDPool()
	pool.Get()
	id2 := pool.Get()
	pool.Release(id2)

	pool.want(&InvokeIDPool{used: map[uint32]bool{}, maxUsed: 1}, t)
}

func TestInvokeIDPool_GetFromUsedSet(t *testing.T) {
	pool := NewInvokeIDPool()
	id1 := pool.Get()
	pool.Get()
	pool.Release(id1)

	if got := pool.Get(); got != 1 {
		t.Errorf("pool.Get() = %v, want 1", got)
	}

	pool.want(&InvokeIDPool{used: map[uint32]bool{}, maxUsed: 2}, t)
}

func wantError(want string, t *testing.T) {
	rec := recover()
	if rec == nil {
		t.Errorf("expected panic, but there wasn't one")
	}
	err, ok := rec.(error)
	if !ok || !strings.Contains(err.Error(), want) {
		t.Errorf("wrong error, got '%v', want '%v'", err, want)
	}
}

func TestInvokeIDPool_Release0(t *testing.T) {
	pool := NewInvokeIDPool()
	pool.Get()

	defer wantError("invalid value", t)
	pool.Release(0)
}

func TestInvokeIDPool_ReleaseInvalid(t *testing.T) {
	pool := NewInvokeIDPool()
	pool.Get()

	defer wantError("invalid value", t)
	pool.Release(5)
}

func TestInvokeIDPool_ReleaseDuplicate(t *testing.T) {
	pool := NewInvokeIDPool()
	pool.Get()
	pool.Get()
	pool.Release(1)

	defer wantError("already recycled", t)
	pool.Release(1)
}
