//go:build integration

package cache_test

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/nfsarch33/engram/internal/cache"
)

// Contains is a new API introduced by q10b-2. The Ginkgo spec is
// RED on main (no implementation), GREEN once the method is added to
// cache.Cache.
//
// TDD contract (L0 rule 42):
//   - RED: this spec fails to compile on main because Contains does not exist.
//   - GREEN: after the implementation lands, this spec passes.
//
// Spec design rationale: Contains is a non-mutating presence check —
// unlike Get, it does NOT refresh LRU recency. This matters for
// observability tooling that wants to ask "is this key present?"
// without polluting the eviction order.
var _ = ginkgo.Describe("cache.Cache Contains", func() {
	var (
		c *cache.Cache
	)

	ginkgo.BeforeEach(func() {
		c = cache.New(cache.Options{MaxEntries: 16})
	})

	ginkgo.When("the key is not present", func() {
		ginkgo.It("returns false without panicking", func() {
			gomega.Expect(c.Contains("missing-key")).To(gomega.BeFalse())
		})
	})

	ginkgo.When("the key is present", func() {
		ginkgo.It("returns true", func() {
			c.Set("k", []byte("v"))
			gomega.Expect(c.Contains("k")).To(gomega.BeTrue())
		})
	})

	ginkgo.When("the key was just invalidated", func() {
		ginkgo.It("returns false again", func() {
			c.Set("k", []byte("v"))
			c.Invalidate("k")
			gomega.Expect(c.Contains("k")).To(gomega.BeFalse())
		})
	})
})