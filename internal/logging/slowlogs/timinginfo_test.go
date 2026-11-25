package slowlogs

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGetTimingInfo_ValidContext(t *testing.T) {
	ctxWithTiming := withTimingInfo(context.Background())

	info := getTimingInfo(ctxWithTiming)

	assert.NotNil(t, info)
	assert.NotNil(t, info.MiddlewareTimings)
	assert.NotNil(t, info.OtherTimings)

	assert.Equal(t, 0, len(info.MiddlewareTimings))
	assert.Equal(t, 0, len(info.OtherTimings))
}

func TestGetTimingInfo_InvalidContext(t *testing.T) {
	info := getTimingInfo(context.Background())

	assert.NotNil(t, info)
	assert.NotNil(t, info.MiddlewareTimings)
	assert.NotNil(t, info.OtherTimings)
	assert.Equal(t, 0, len(info.MiddlewareTimings))
	assert.Equal(t, 0, len(info.OtherTimings))
}

func TestRecorder(t *testing.T) {
	ctx := withTimingInfo(context.Background())
	functionName := "testFunction"

	recorder := Recorder(ctx, functionName)
	time.Sleep(100 * time.Millisecond) // Simulate function execution time
	recorder()

	info := getTimingInfo(ctx)
	assert.Contains(t, info.OtherTimings, functionName)

	duration := info.OtherTimings[functionName]
	assert.GreaterOrEqual(t, duration, 100*time.Millisecond)
}

func TestRecordOtherTiming(t *testing.T) {
	ctx := withTimingInfo(context.Background())
	functionName := "testHandler"
	duration := 200 * time.Millisecond

	recordOtherTiming(ctx, functionName, duration)

	info := getTimingInfo(ctx)
	assert.Contains(t, info.OtherTimings, functionName)
	assert.Equal(t, duration, info.OtherTimings[functionName])
}
