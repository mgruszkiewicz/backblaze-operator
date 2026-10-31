package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	b2v1alpha2 "github.com/mgruszkiewicz/backblaze-operator/api/v1alpha2"
	"github.com/mgruszkiewicz/go-backblaze"
)

// Bucket E2E: exercises the real operator image on a kind cluster against
// the real B2 API. Fill in the TODO assertions for your settings
// (acl, lifecycle rules) — see helpers.go and the guide in chat.
var _ = Describe("Bucket E2E", Label("bucket", "e2e"), func() {
	var ctx context.Context
	var namespace string

	BeforeEach(func() {
		ctx = context.Background()
		namespace = createTestNamespace(ctx)
	})

	It("creates a private bucket at B2 and reports Ready", func() {
		requireB2Creds()
		b2 := newB2Client()
		bucketName := uniqueName("e2e-bkt")

		bucket := &b2v1alpha2.Bucket{
			ObjectMeta: metav1.ObjectMeta{Name: bucketName, Namespace: namespace},
			Spec: b2v1alpha2.BucketSpec{
				AtProvider: b2v1alpha2.BucketSpecAtProvider{Acl: "private"},
			},
		}
		Expect(k8sClient.Create(ctx, bucket)).To(Succeed())

		// Cleanup: deleting the CR must delete at B2 (finalizer path).
		// Buckets must be empty before B2 allows deletion.
		DeferCleanup(func(ctx context.Context) {
			obj := &b2v1alpha2.Bucket{}
			if err := k8sClient.Get(ctx, namespacedName(namespace, bucketName), obj); err == nil {
				_ = k8sClient.Delete(ctx, obj)
			}
		})

		// 1. K8s-side: operator reconciled.
		got := waitForBucketReady(ctx, namespace, bucketName)
		Expect(got.Status.AtProvider.Acl).To(Equal("private"))

		// 2. Provider-side: bucket really exists at B2 with correct settings.
		live_at_provider, err := b2.Bucket(bucketName)
		Expect(err).NotTo(HaveOccurred())
		Expect(live_at_provider).NotTo(BeNil())
		Expect(string(live_at_provider.BucketType)).To(ContainSubstring("allPrivate"))

		// 3. update path — change Spec, waitForBucketReady again,
		// re-assert at B2.
		//
		updated_lifecycle_rules := getBucket(ctx, namespace, bucketName)
		updated_lifecycle_rules.Spec.AtProvider.BucketLifecycle = []backblaze.LifecycleRule{
			{FileNamePrefix: "/templogs", DaysFromHidingToDeleting: 6, DaysFromUploadingToHiding: 7},
		}
		Expect(k8sClient.Update(ctx, updated_lifecycle_rules)).To(Succeed())
		// waitForBucketReady alone is not enough here: Ready is already True
		// from step 1, so it returns before the controller sees the new Spec.
		// Wait for Status to converge first, then check B2 (also eventually
		// consistent, so poll it too).
		Eventually(func(g Gomega) {
			st := getBucket(ctx, namespace, bucketName)
			g.Expect(st.Status.AtProvider.BucketLifecycle).To(HaveLen(1))
		}, eventuallyTimeout, eventuallyPoll).Should(Succeed())

		Eventually(func(g Gomega) {
			liveUpdated, err := b2.Bucket(bucketName)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(liveUpdated).NotTo(BeNil())
			g.Expect(liveUpdated.LifecycleRules).To(HaveLen(1))
			g.Expect(liveUpdated.LifecycleRules[0].FileNamePrefix).To(Equal("/templogs"))
			g.Expect(liveUpdated.LifecycleRules[0].DaysFromHidingToDeleting).To(Equal(6))
			g.Expect(liveUpdated.LifecycleRules[0].DaysFromUploadingToHiding).To(Equal(7))
		}, eventuallyTimeout, eventuallyPoll).Should(Succeed())

		// 4. delete path — Delete the CR, waitForDeletion,
		fresh := getBucket(ctx, namespace, bucketName)
		Expect(k8sClient.Delete(ctx, fresh)).To(Succeed())
		waitForDeletion(ctx, fresh)
		// validate that the bucket is not existing at the provider
		live_after_deletion, err := b2.Bucket(bucketName)
		Expect(err).NotTo(HaveOccurred())
		Expect(live_after_deletion).To(BeNil())

	})
})
