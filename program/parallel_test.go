package program

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/go-multierror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParallel_EmptyList(t *testing.T) {
	called := int32(0)
	err := parallel[int](nil, func(int) error {
		atomic.AddInt32(&called, 1)
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, int32(0), called)
}

func TestParallel_AllSuccess(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	var seen sync.Map
	err := parallel[int](items, func(i int) error {
		seen.Store(i, true)
		return nil
	})
	assert.NoError(t, err)
	for _, i := range items {
		_, ok := seen.Load(i)
		assert.True(t, ok, "item %d was not processed", i)
	}
}

func TestParallel_AllErrors(t *testing.T) {
	items := []int{1, 2, 3}
	err := parallel[int](items, func(i int) error {
		return errors.New("fail")
	})
	require.Error(t, err)

	merr, ok := err.(*multierror.Error)
	require.True(t, ok, "expected *multierror.Error, got %T", err)
	assert.Len(t, merr.Errors, len(items))
}

func TestParallel_MixedSuccessAndError(t *testing.T) {
	items := []int{1, 2, 3, 4}
	err := parallel[int](items, func(i int) error {
		if i%2 == 0 {
			return errors.New("even")
		}
		return nil
	})
	require.Error(t, err)
	merr, ok := err.(*multierror.Error)
	require.True(t, ok)
	assert.Len(t, merr.Errors, 2)
}

func TestParallel_RaceSafety(t *testing.T) {
	// Run a large fan-out under -race to catch obvious concurrency bugs.
	const n = 500
	items := make([]int, n)
	for i := range items {
		items[i] = i
	}

	var counter int32
	err := parallel[int](items, func(i int) error {
		atomic.AddInt32(&counter, 1)
		return nil
	})
	assert.NoError(t, err)
	assert.Equal(t, int32(n), atomic.LoadInt32(&counter))
}
