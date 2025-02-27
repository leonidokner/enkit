package service

import (
	"strings"
	"time"

	fpb "github.com/enfabrica/enkit/flextape/proto"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// license manages allocations and queued invocations for a single license
// type.
type license struct {
	totalAvailable int                    // Constant total number of licenses available for invocations.
	queue          []*invocation          // List of invocations waiting for a license, in FIFO order.
	allocations    map[string]*invocation // Map of invocation ID to invocation data for an allocated license.
}

// formatLicenseType returns a unique string for a particular vendor/feature
// combination.
func formatLicenseType(l *fpb.License) string {
	return strings.Join([]string{l.GetVendor(), l.GetFeature()}, "::")
}

// Enqueue puts the supplied invocation at the back of the queue. Returns the
// 1-based index the invocation was queued at.
func (l *license) Enqueue(inv *invocation) uint32 {
	l.queue = append(l.queue, inv)
	return uint32(len(l.queue))
}

// Allocate attempts to associate the supplied invocation with a license, if
// one is available. Returns whether a license was successfully allocated.
func (l *license) Allocate(inv *invocation) bool {
	if len(l.allocations) >= l.totalAvailable {
		return false
	}
	l.allocations[inv.ID] = inv
	return true
}

// Promote attempts to promote queued requests to allocations until either no
// licenses remain or no queued requests remain.
func (l *license) Promote() {
	numFree := l.totalAvailable - len(l.allocations)
	numAllocated := 0
	for i := 0; i < numFree && i < len(l.queue); i++ {
		l.allocations[l.queue[i].ID] = l.queue[i]
		numAllocated++
	}
	l.queue = l.queue[numAllocated:]
}

// GetAllocated returns an invocation by ID if the invocation is allocated a
// license, or nil otherwise.
func (l *license) GetAllocated(invID string) *invocation {
	return l.allocations[invID]
}

// ExpireAllocations removes all allocations for invocations that have not
// checked in since `expiry`.
func (l *license) ExpireAllocations(expiry time.Time) {
	newAllocations := map[string]*invocation{}
	for k, v := range l.allocations {
		if v.LastCheckin.After(expiry) {
			newAllocations[k] = v
		}
	}
	l.allocations = newAllocations
}

// ExpireQueued removes all queued invocations that have not checked in since
// `expiry`.
func (l *license) ExpireQueued(expiry time.Time) {
	newQueued := []*invocation{}
	for _, inv := range l.queue {
		if inv.LastCheckin.After(expiry) {
			newQueued = append(newQueued, inv)
		}
	}
	l.queue = newQueued
}

// GetQueued returns an invocation by ID if the invocation is queued, or nil
// otherwise. If the returned invocation is not nil, the 1-based index (queue
// position) is also returned.
func (l *license) GetQueued(invID string) (*invocation, uint32) {
	for i, inv := range l.queue {
		if inv.ID == invID {
			return inv, uint32(i+1)
		}
	}
	return nil, 0
}

// GetStats returns a LicenseStats message for this license type.
func (l *license) GetStats(name string) *fpb.LicenseStats {
	fields := strings.SplitN(name, "::", 2)
	if len(fields) != 2 {
		fields = []string{"<UNKNOWN>", name}
	}
	return &fpb.LicenseStats{
		License: &fpb.License{
			Vendor:  fields[0],
			Feature: fields[1],
		},
		Timestamp:         timestamppb.New(timeNow()),
		TotalLicenseCount: uint32(l.totalAvailable),
		AllocatedCount:    uint32(len(l.allocations)),
		QueuedCount:       uint32(len(l.queue)),
	}
}

// Forget removes invocations matching the specified ID from allocations and
// the queue.
func (l *license) Forget(invID string) int {
	count := 0
	newAllocations := map[string]*invocation{}
	for k, v := range l.allocations {
		if k != invID {
			newAllocations[k] = v
		} else {
			count++
		}
	}

	newQueue := []*invocation{}
	for _, inv := range l.queue {
		if inv.ID != invID {
			newQueue = append(newQueue, inv)
		} else {
			count++
		}
	}

	l.allocations = newAllocations
	l.queue = newQueue
	return count
}
