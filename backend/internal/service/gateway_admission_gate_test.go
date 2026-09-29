//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGatewayAdmissionGate_DisabledByDefault(t *testing.T) {
	require.Nil(t, NewGatewayAdmissionGate(GatewayAdmissionConfig{}),
		"max_inflight=0 must produce no gate at all, so an unconfigured deployment keeps its behaviour")
	gate := NewGatewayAdmissionGate(GatewayAdmissionConfig{MaxInflight: 0, MaxWaiting: 5, WaitTimeout: time.Second})
	require.Nil(t, gate)

	var nilGate *GatewayAdmissionGate
	release, err := nilGate.Acquire(context.Background())
	require.NoError(t, err)
	require.NotNil(t, release)
	release()
	require.Equal(t, 0, nilGate.Inflight())
}

func TestGatewayAdmissionGate_BoundedQueueShedsLoad(t *testing.T) {
	gate := NewGatewayAdmissionGate(GatewayAdmissionConfig{
		MaxInflight: 1,
		MaxWaiting:  1,
		WaitTimeout: 5 * time.Second,
	})

	firstRelease, err := gate.Acquire(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, gate.Inflight())

	// 第二个请求进入有界等待队列；释放后可拿到槽位。
	var wg sync.WaitGroup
	wg.Add(1)
	var secondErr error
	go func() {
		defer wg.Done()
		release, acquireErr := gate.Acquire(context.Background())
		secondErr = acquireErr
		if release != nil {
			release()
		}
	}()

	require.Eventually(t, func() bool { return gate.Waiting() == 1 }, time.Second, time.Millisecond,
		"the second request must be queued, not inflight")
	firstRelease()
	wg.Wait()
	require.NoError(t, secondErr, "a queued request proceeds once capacity returns")
	require.Equal(t, 0, gate.Waiting())

	// 有界队列已满：后面的请求立即得到过载，不会无限堆积。
	held, err := gate.Acquire(context.Background())
	require.NoError(t, err)
	queuedDone := make(chan error, 1)
	go func() {
		release, acquireErr := gate.Acquire(context.Background())
		if release != nil {
			release()
		}
		queuedDone <- acquireErr
	}()
	require.Eventually(t, func() bool { return gate.Waiting() == 1 }, time.Second, time.Millisecond)

	_, err = gate.Acquire(context.Background())
	require.ErrorIs(t, err, ErrGatewayOverloaded, "a full queue is overload, not a silent pile-up")
	require.Equal(t, 1, gate.Waiting(), "the rejection must not consume a queue slot")

	held()
	require.NoError(t, <-queuedDone, "the queued request proceeds once the holder releases")
	require.Eventually(t, func() bool { return gate.Waiting() == 0 }, time.Second, time.Millisecond)
}

func TestGatewayAdmissionGate_WaitTimeoutSurfacesAsOverload(t *testing.T) {
	gate := NewGatewayAdmissionGate(GatewayAdmissionConfig{
		MaxInflight: 1,
		MaxWaiting:  2,
		WaitTimeout: 20 * time.Millisecond,
	})
	release, err := gate.Acquire(context.Background())
	require.NoError(t, err)
	defer release()

	_, err = gate.Acquire(context.Background())
	require.ErrorIs(t, err, ErrGatewayAdmissionTimeout)
	require.Equal(t, 0, gate.Waiting(), "a timed-out waiter must free its queue slot")
}

func TestGatewayAdmissionGate_CancelReleasesWaiter(t *testing.T) {
	gate := NewGatewayAdmissionGate(GatewayAdmissionConfig{
		MaxInflight: 1,
		MaxWaiting:  2,
		WaitTimeout: 5 * time.Second,
	})
	release, err := gate.Acquire(context.Background())
	require.NoError(t, err)
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, acquireErr := gate.Acquire(ctx)
		done <- acquireErr
	}()
	require.Eventually(t, func() bool { return gate.Waiting() == 1 }, time.Second, time.Millisecond)

	cancel()
	select {
	case acquireErr := <-done:
		require.ErrorIs(t, acquireErr, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("a cancelled request must stop waiting")
	}
	require.Equal(t, 0, gate.Waiting(), "cancel must release the queue slot")
	require.Equal(t, 1, gate.Inflight(), "the holder still owns its slot")
}

func TestGatewayAdmissionGate_AcquireForPlanReleasesOnFinalize(t *testing.T) {
	gate := NewGatewayAdmissionGate(GatewayAdmissionConfig{MaxInflight: 1, MaxWaiting: 1, WaitTimeout: time.Second})
	plan := NewGatewayRequestPlan(GatewayRequestSpec{})

	require.NoError(t, gate.AcquireForPlan(context.Background(), plan))
	require.Equal(t, 1, gate.Inflight())

	// 第二次为同一计划取用会再占一个槽位（调用方按一次一取使用）；
	// 这里只验证计划结束时全部归还。
	plan.Finalize(false)
	require.Equal(t, 0, gate.Inflight(), "finalizing the request returns the instance capacity")

	finished := NewGatewayRequestPlan(GatewayRequestSpec{})
	finished.Cancel()
	require.NoError(t, gate.AcquireForPlan(context.Background(), finished),
		"acquiring for an already-finished plan must not fail the request")
	require.Equal(t, 0, gate.Inflight(), "a late acquisition is released immediately")
}

func TestGatewayAdmissionGate_AcquireWithoutPlanReleasesOnContextEnd(t *testing.T) {
	gate := NewGatewayAdmissionGate(GatewayAdmissionConfig{MaxInflight: 1, MaxWaiting: 0, WaitTimeout: 0})
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, gate.AcquireForPlan(ctx, nil))
	require.Equal(t, 1, gate.Inflight())
	cancel()
	require.Eventually(t, func() bool { return gate.Inflight() == 0 }, time.Second, time.Millisecond,
		"requests without a plan still release capacity when their context ends")
}

func TestGatewayAdmissionGate_OverloadErrorsAreServiceUnavailable(t *testing.T) {
	require.Equal(t, int32(503), ErrGatewayOverloaded.Code)
	require.Equal(t, int32(503), ErrGatewayAdmissionTimeout.Code)
	require.True(t, errors.Is(ErrGatewayOverloaded, ErrGatewayOverloaded))
}
