// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package store

import (
	"testing"

	"github.com/efficientgo/core/testutil"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/thanos/pkg/dedup"
	"github.com/thanos-io/thanos/pkg/errors"
	"github.com/thanos-io/thanos/pkg/store/storepb"
)

func TestRmLabelsCornerCases(t *testing.T) {
	testutil.Equals(t, rmLabels(labelsFromStrings("aa", "bb"), map[string]struct{}{
		"aa": {},
	}), labels.Labels{})
	testutil.Equals(t, rmLabels(labelsFromStrings(), map[string]struct{}{
		"aa": {},
	}), labels.Labels{})
}

type testingRespSet struct {
	bufferedResponse []*storepb.SeriesResponse
	i                int
}

func (l *testingRespSet) Close() {}

func (l *testingRespSet) At() *storepb.SeriesResponse {
	return l.bufferedResponse[l.i]
}

func (l *testingRespSet) Next() bool {
	l.i++
	return l.i < len(l.bufferedResponse)
}

func (l *testingRespSet) StoreID() string {
	return ""
}

func (l *testingRespSet) Labelset() string {
	return ""
}

func (l *testingRespSet) Empty() bool {
	return l.i >= len(l.bufferedResponse)
}

func TestProxyResponseHeapSort(t *testing.T) {
	for _, tcase := range []struct {
		input []respSet
		exp   []*storepb.SeriesResponse
	}{
		// Different sorted response sets
		{
			input: []respSet{
				&testingRespSet{
					bufferedResponse: []*storepb.SeriesResponse{
						storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3")),
						storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3", "d", "4")),
					},
				},
				&testingRespSet{
					bufferedResponse: []*storepb.SeriesResponse{
						storeSeriesResponse(t, labelsFromStrings("a", "1", "d", "4")),
						storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "4", "e", "5")),
					},
				},
			},
			exp: []*storepb.SeriesResponse{
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "d", "4")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3", "d", "4")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "4", "e", "5")),
			},
		},
		// Sorted response sets with different number of labels in their series
		{
			input: []respSet{
				&testingRespSet{
					bufferedResponse: []*storepb.SeriesResponse{
						storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "2", "c", "3")),
						storeSeriesResponse(t, labelsFromStrings("b", "2", "c", "3")),
					},
				},
				&testingRespSet{
					bufferedResponse: []*storepb.SeriesResponse{
						storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "2")),
						storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "2", "e", "5")),
					},
				},
			},
			exp: []*storepb.SeriesResponse{
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "2")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "2", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "2", "e", "5")),
				storeSeriesResponse(t, labelsFromStrings("b", "2", "c", "3")),
			},
		},
		// Duplicated response sets that are sorted if you remove common label-values
		// This is similar to response sets from stores that sort the set before adding external labels
		{
			input: []respSet{
				&testingRespSet{
					bufferedResponse: []*storepb.SeriesResponse{
						storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3")),
						storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "2", "c", "3")),
					},
				},
				&testingRespSet{
					bufferedResponse: []*storepb.SeriesResponse{
						storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3")),
						storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "2", "c", "3")),
					},
				},
			},
			exp: []*storepb.SeriesResponse{
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "2", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "2", "c", "3")),
			},
		},
	} {
		t.Run("", func(t *testing.T) {
			h := NewProxyResponseHeap(tcase.input...)
			if !h.Empty() {
				got := []*storepb.SeriesResponse{h.At()}
				for h.Next() {
					got = append(got, h.At())
				}
				testutil.Equals(t, tcase.exp, got)
			}
		})
	}
}

func TestSortWithoutLabels(t *testing.T) {
	for _, tcase := range []struct {
		input       []*storepb.SeriesResponse
		exp         []*storepb.SeriesResponse
		dedupLabels map[string]struct{}
	}{
		// Single deduplication label.
		{
			input: []*storepb.SeriesResponse{
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "replica-1", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "replica-1", "c", "3", "d", "4")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "replica-1", "c", "4")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "replica-2", "c", "3")),
			},
			exp: []*storepb.SeriesResponse{
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3", "d", "4")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "4")),
			},
			dedupLabels: map[string]struct{}{"b": {}},
		},
		// Multi deduplication labels.
		{
			input: []*storepb.SeriesResponse{
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "replica-1", "b1", "replica-1", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "replica-1", "b1", "replica-1", "c", "3", "d", "4")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "replica-1", "b1", "replica-1", "c", "4")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "replica-1", "b1", "replica-2", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "replica-2", "c", "3")),
			},
			exp: []*storepb.SeriesResponse{
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3", "d", "4")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "4")),
			},
			dedupLabels: map[string]struct{}{"b": {}, "b1": {}},
		},
		// Pushdown label at the end.
		{
			input: []*storepb.SeriesResponse{
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "replica-1", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "replica-1", "c", "3", "d", "4")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "replica-1", "c", "4", dedup.PushdownMarker.Name, dedup.PushdownMarker.Value)),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "replica-2", "c", "3")),
			},
			exp: []*storepb.SeriesResponse{
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3", "d", "4")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "4", dedup.PushdownMarker.Name, dedup.PushdownMarker.Value)),
			},
			dedupLabels: map[string]struct{}{"b": {}},
		},
		// Non series responses mixed.
		{
			input: []*storepb.SeriesResponse{
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "replica-1", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "replica-1", "c", "3", "d", "4")),
				storepb.NewWarnSeriesResponse(errors.Newf("yolo")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "replica-1", "c", "4")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "b", "replica-2", "c", "3")),
			},
			exp: []*storepb.SeriesResponse{
				storepb.NewWarnSeriesResponse(errors.Newf("yolo")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "3", "d", "4")),
				storeSeriesResponse(t, labelsFromStrings("a", "1", "c", "4")),
			},
			dedupLabels: map[string]struct{}{"b": {}},
		},
		// Longer series.
		{
			input: []*storepb.SeriesResponse{
				storeSeriesResponse(t, labels.FromStrings(
					"__name__", "gitlab_transaction_cache_read_hit_count_total", "action", "widget.json", "controller", "Projects::MergeRequests::ContentController", "env", "gprd", "environment",
					"gprd", "fqdn", "web-08-sv-gprd.c.gitlab-production.internal", "instance", "web-08-sv-gprd.c.gitlab-production.internal:8083", "job", "gitlab-rails", "monitor", "app", "provider",
					"gcp", "region", "us-east", "replica", "01", "shard", "default", "stage", "main", "tier", "sv", "type", "web",
				)),
			},
			exp: []*storepb.SeriesResponse{
				storeSeriesResponse(t, labels.FromStrings(
					// No replica label anymore.
					"__name__", "gitlab_transaction_cache_read_hit_count_total", "action", "widget.json", "controller", "Projects::MergeRequests::ContentController", "env", "gprd", "environment",
					"gprd", "fqdn", "web-08-sv-gprd.c.gitlab-production.internal", "instance", "web-08-sv-gprd.c.gitlab-production.internal:8083", "job", "gitlab-rails", "monitor", "app", "provider",
					"gcp", "region", "us-east", "shard", "default", "stage", "main", "tier", "sv", "type", "web",
				)),
			},
			dedupLabels: map[string]struct{}{"replica": {}},
		},
	} {
		t.Run("", func(t *testing.T) {
			sortWithoutLabels(tcase.input, tcase.dedupLabels)
			testutil.Equals(t, tcase.exp, tcase.input)
		})
	}
}

// labelsFromStrings is like labels.FromString, but it does not sort the input.
func labelsFromStrings(ss ...string) labels.Labels {
	if len(ss)%2 != 0 {
		panic("invalid number of strings")
	}
	res := make(labels.Labels, 0, len(ss)/2)
	for i := 0; i < len(ss); i += 2 {
		res = append(res, labels.Label{Name: ss[i], Value: ss[i+1]})
	}

	return res
}

func BenchmarkSortWithoutLabels(b *testing.B) {
	resps := make([]*storepb.SeriesResponse, 1e4)
	labelsToRemove := map[string]struct{}{
		"a": {}, "b": {},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		for i := 0; i < 1e4; i++ {
			resps[i] = storeSeriesResponse(b, labels.FromStrings("a", "1", "b", "replica-1", "c", "replica-1", "d", "1"))
		}
		b.StartTimer()
		sortWithoutLabels(resps, labelsToRemove)
	}
}
