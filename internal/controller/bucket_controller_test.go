package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	backblaze "github.com/mgruszkiewicz/go-backblaze"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	b2v1alpha2 "github.com/mgruszkiewicz/backblaze-operator/api/v1alpha2"
)

func newBucketReconciler(fake *fakeB2, recorder record.EventRecorder) *BucketReconciler {
	return &BucketReconciler{
		Client:        k8sClient,
		Scheme:        scheme.Scheme,
		Backblaze:     fake,
		EventRecorder: recorder,
	}
}

func reconcileBucket(r *BucketReconciler, name string) (ctrl.Result, error) {
	return r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: "default"},
	})
}

func newBucket(name string) *b2v1alpha2.Bucket {
	return &b2v1alpha2.Bucket{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: b2v1alpha2.BucketSpec{
			AtProvider: b2v1alpha2.BucketSpecAtProvider{Acl: "private"},
		},
	}
}

func getBucket(name string) *b2v1alpha2.Bucket {
	bucket := &b2v1alpha2.Bucket{}
	Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, bucket)).To(Succeed())
	return bucket
}

var _ = Describe("Bucket controller", func() {
	ctx := context.Background()

	It("creates the bucket at the provider and reports Ready", func() {
		fake := &fakeB2{
			createBucketResp: &backblaze.Bucket{BucketInfo: &backblaze.BucketInfo{ID: "bucket-id-10", Name: "bucket-create"}},
		}
		r := newBucketReconciler(fake, nil)

		Expect(k8sClient.Create(ctx, newBucket("bucket-create"))).To(Succeed())

		_, err := reconcileBucket(r, "bucket-create")
		Expect(err).NotTo(HaveOccurred())
		_, err = reconcileBucket(r, "bucket-create")
		Expect(err).NotTo(HaveOccurred())

		bucket := getBucket("bucket-create")
		Expect(bucket.Status.Reconciled).To(BeTrue())
		cond := meta.FindStatusCondition(bucket.Status.Conditions, ConditionTypeReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		Expect(fake.createdBuckets).To(ContainElement("bucket-create"))
	})

	It("reports DuplicateBucketName with the real API error when the name is taken", func() {
		recorder := record.NewFakeRecorder(10)
		fake := &fakeB2{
			createBucketErr: &backblaze.B2Error{Code: "duplicate_bucket_name", Message: "Bucket name is already in use.", Status: 400},
		}
		r := newBucketReconciler(fake, recorder)

		Expect(k8sClient.Create(ctx, newBucket("bucket-duplicate"))).To(Succeed())

		_, err := reconcileBucket(r, "bucket-duplicate")
		Expect(err).NotTo(HaveOccurred())

		_, err = reconcileBucket(r, "bucket-duplicate")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("duplicate_bucket_name"))

		bucket := getBucket("bucket-duplicate")
		Expect(bucket.Status.Reconciled).To(BeFalse())
		cond := meta.FindStatusCondition(bucket.Status.Conditions, ConditionTypeReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(ReasonDuplicateBucketName))
		Expect(cond.Message).To(ContainSubstring("Bucket name is already in use"))

		Eventually(recorder.Events).Should(Receive(ContainSubstring(ReasonDuplicateBucketName)))
	})

	It("reports BucketCreationFailed for other provider errors", func() {
		fake := &fakeB2{
			createBucketErr: &backblaze.B2Error{Code: "no_payment_history", Message: "Please add a payment method.", Status: 403},
		}
		r := newBucketReconciler(fake, nil)

		Expect(k8sClient.Create(ctx, newBucket("bucket-payment"))).To(Succeed())

		_, err := reconcileBucket(r, "bucket-payment")
		Expect(err).NotTo(HaveOccurred())
		_, err = reconcileBucket(r, "bucket-payment")
		Expect(err).To(HaveOccurred())

		cond := meta.FindStatusCondition(getBucket("bucket-payment").Status.Conditions, ConditionTypeReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal(ReasonBucketCreateFailed))
		Expect(cond.Message).To(ContainSubstring("no_payment_history"))
	})

	It("adopts a bucket that already exists on the account", func() {
		fake := &fakeB2{
			bucket: &backblaze.Bucket{BucketInfo: &backblaze.BucketInfo{ID: "bucket-id-11", Name: "bucket-adopt"}},
		}
		r := newBucketReconciler(fake, nil)

		Expect(k8sClient.Create(ctx, newBucket("bucket-adopt"))).To(Succeed())

		_, err := reconcileBucket(r, "bucket-adopt")
		Expect(err).NotTo(HaveOccurred())
		_, err = reconcileBucket(r, "bucket-adopt")
		Expect(err).NotTo(HaveOccurred())

		bucket := getBucket("bucket-adopt")
		Expect(bucket.Status.Reconciled).To(BeTrue())
		Expect(fake.createdBuckets).To(BeEmpty())
	})

	It("keeps the finalizer when the provider lookup fails during deletion", func() {
		fake := &fakeB2{
			createBucketResp: &backblaze.Bucket{BucketInfo: &backblaze.BucketInfo{ID: "bucket-id-12", Name: "bucket-delete-err"}},
		}
		r := newBucketReconciler(fake, nil)

		Expect(k8sClient.Create(ctx, newBucket("bucket-delete-err"))).To(Succeed())
		_, err := reconcileBucket(r, "bucket-delete-err")
		Expect(err).NotTo(HaveOccurred())
		_, err = reconcileBucket(r, "bucket-delete-err")
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Delete(ctx, getBucket("bucket-delete-err"))).To(Succeed())

		// Transient provider error must not be mistaken for "bucket gone":
		// the finalizer stays and the deletion is retried.
		fake.bucketErr = &backblaze.B2Error{Code: "service_unavailable", Message: "try again", Status: 503}
		_, err = reconcileBucket(r, "bucket-delete-err")
		Expect(err).To(HaveOccurred())
		Expect(controllerutil.ContainsFinalizer(getBucket("bucket-delete-err"), bucketFinalizer)).To(BeTrue())

		// Once the provider confirms the bucket is gone, the finalizer is removed.
		fake.bucketErr = nil
		fake.bucket = nil
		_, err = reconcileBucket(r, "bucket-delete-err")
		Expect(err).NotTo(HaveOccurred())
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "bucket-delete-err", Namespace: "default"}, &b2v1alpha2.Bucket{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})
})
