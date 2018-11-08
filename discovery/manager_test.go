// Package discovery manages and runs service discovery and saves target
// configuration files.
package discovery

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type fakeLiteral struct{}

func (f *fakeLiteral) Discover(ctx context.Context) ([]StaticConfig, error) {
	return []StaticConfig{
		{Targets: []string{"output"}, Labels: map[string]string{"key": "value"}},
	}, nil
}

type fakeTimeout struct{}

func (f *fakeTimeout) Discover(ctx context.Context) ([]StaticConfig, error) {
	<-ctx.Done()
	return []StaticConfig{}, nil
}

type fakeFailure struct{}

func (f *fakeFailure) Discover(ctx context.Context) ([]StaticConfig, error) {
	return nil, fmt.Errorf("Failed to discover")
}

func TestManager_Run(t *testing.T) {
	tests := []struct {
		name     string
		service  Service
		output   string
		timeout  time.Duration
		ctx      context.Context
		interval time.Duration
	}{
		{
			name:    "success",
			service: &fakeLiteral{},
			output:  "foo.txt",
			timeout: time.Minute,
		},
		{
			name:    "failure-cannot-write",
			service: &fakeLiteral{},
			output:  "/path/does/not/exist/foo.txt",
			timeout: time.Minute,
		},
		{
			name:    "failure-timeout",
			service: &fakeTimeout{},
			output:  "foo.txt",
			timeout: time.Second,
		},
		{
			name:    "failure-to-discovery",
			service: &fakeFailure{},
			timeout: time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			m := NewManager(tt.timeout)
			m.Register(tt.service, tt.output)
			if m.Count() != 1 {
				t.Errorf("Wrong manager count; got %q, want 1", m.Count())
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Second/2)
			defer cancel()
			m.Run(ctx, time.Second/2)
		})
	}
}
