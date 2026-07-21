package controller

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	backblaze "github.com/mgruszkiewicz/go-backblaze"
	corev1 "k8s.io/api/core/v1"
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

func newKeyReconciler(fake *fakeB2, recorder record.EventRecorder) *KeyReconciler {
	return &KeyReconciler{
		Client:        k8sClient,
		Scheme:        scheme.Scheme,
		Backblaze:     fake,
		EventRecorder: recorder,
	}
}

func reconcileKey(r *KeyReconciler, name string) (ctrl.Result, error) {
	return r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: name, Namespace: "default"},
	})
}

func newKey(name, bucketName, secretName string) *b2v1alpha2.Key {
	return &b2v1alpha2.Key{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: b2v1alpha2.KeySpec{
			AtProvider: b2v1alpha2.KeySpecAtProvider{
				BucketName:   bucketName,
				Capabilities: []string{"listBuckets", "listFiles"},
			},
			WriteConnectionSecretToRef: b2v1alpha2.WriteConnectionSecretToRef{Name: secretName},
		},
	}
}

func getKey(name string) *b2v1alpha2.Key {
	key := &b2v1alpha2.Key{}
	Expect(k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: "default"}, key)).To(Succeed())
	return key
}

var _ = Describe("Key controller", func() {
	ctx := context.Background()

	It("adds the finalizer on first reconcile", func() {
		key := newKey("key-finalizer", "some-bucket", "secret-key-finalizer")
		Expect(k8sClient.Create(ctx, key)).To(Succeed())

		_, err := reconcileKey(newKeyReconciler(&fakeB2{}, nil), "key-finalizer")
		Expect(err).NotTo(HaveOccurred())
		Expect(controllerutil.ContainsFinalizer(getKey("key-finalizer"), keyFinalizer)).To(BeTrue())
	})

	It("does not panic when the referenced bucket does not exist, requeues with an error and reports BucketNotFound", func() {
		// Regression test: the zero-value fake reproduces go-backblaze's
		// (nil, nil) "bucket not found" contract that used to crash the
		// operator with a nil pointer dereference.
		recorder := record.NewFakeRecorder(10)
		r := newKeyReconciler(&fakeB2{}, recorder)

		key := newKey("key-missing-bucket", "bucket-does-not-exist", "secret-key-missing-bucket")
		Expect(k8sClient.Create(ctx, key)).To(Succeed())

		_, err := reconcileKey(r, "key-missing-bucket")
		Expect(err).NotTo(HaveOccurred()) // finalizer pass

		_, err = reconcileKey(r, "key-missing-bucket")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not found at provider"))

		key = getKey("key-missing-bucket")
		Expect(key.Status.Reconciled).To(BeFalse())
		cond := meta.FindStatusCondition(key.Status.Conditions, ConditionTypeReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(ReasonBucketNotFound))

		Eventually(recorder.Events).Should(Receive(ContainSubstring(ReasonBucketNotFound)))
	})

	It("returns an error and reports ProviderError when the bucket lookup fails", func() {
		r := newKeyReconciler(&fakeB2{bucketErr: &backblaze.B2Error{Code: "bad_auth_token", Message: "auth", Status: 401}}, nil)

		key := newKey("key-provider-error", "some-bucket", "secret-key-provider-error")
		Expect(k8sClient.Create(ctx, key)).To(Succeed())

		_, err := reconcileKey(r, "key-provider-error")
		Expect(err).NotTo(HaveOccurred())

		_, err = reconcileKey(r, "key-provider-error")
		Expect(err).To(HaveOccurred())

		cond := meta.FindStatusCondition(getKey("key-provider-error").Status.Conditions, ConditionTypeReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal(ReasonProviderError))
	})

	It("returns an error and reports KeyCreationFailed when the provider rejects the key", func() {
		fake := &fakeB2{
			bucket:       &backblaze.Bucket{BucketInfo: &backblaze.BucketInfo{ID: "bucket-id-1", Name: "some-bucket"}},
			createKeyErr: &backblaze.B2Error{Code: "bad_request", Message: "invalid capability", Status: 400},
		}
		r := newKeyReconciler(fake, nil)

		key := newKey("key-create-failed", "some-bucket", "secret-key-create-failed")
		Expect(k8sClient.Create(ctx, key)).To(Succeed())

		_, err := reconcileKey(r, "key-create-failed")
		Expect(err).NotTo(HaveOccurred())

		_, err = reconcileKey(r, "key-create-failed")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unable to create application key"))

		key = getKey("key-create-failed")
		Expect(key.Status.Reconciled).To(BeFalse())
		cond := meta.FindStatusCondition(key.Status.Conditions, ConditionTypeReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal(ReasonKeyCreationFailed))
	})

	It("creates the key and its secret when the bucket exists", func() {
		fake := &fakeB2{
			bucket: &backblaze.Bucket{BucketInfo: &backblaze.BucketInfo{ID: "bucket-id-2", Name: "existing-bucket"}},
			createKeyResp: &backblaze.ApplicationKeyResponse{
				KeyName:          "key-happy",
				ApplicationKeyId: "app-key-id-1",
				ApplicationKey:   "app-key-secret",
			},
		}
		r := newKeyReconciler(fake, nil)

		key := newKey("key-happy", "existing-bucket", "secret-key-happy")
		Expect(k8sClient.Create(ctx, key)).To(Succeed())

		_, err := reconcileKey(r, "key-happy")
		Expect(err).NotTo(HaveOccurred())
		_, err = reconcileKey(r, "key-happy")
		Expect(err).NotTo(HaveOccurred())

		key = getKey("key-happy")
		Expect(key.Status.Reconciled).To(BeTrue())
		Expect(key.Status.KeyId).To(Equal("app-key-id-1"))
		cond := meta.FindStatusCondition(key.Status.Conditions, ConditionTypeReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		Expect(cond.Reason).To(Equal(ReasonReconciled))

		secret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "secret-key-happy", Namespace: "default"}, secret)).To(Succeed())
		Expect(secret.Data).To(HaveKeyWithValue("AWS_ACCESS_KEY_ID", []byte("app-key-id-1")))
		Expect(secret.Data).To(HaveKeyWithValue("AWS_SECRET_ACCESS_KEY", []byte("app-key-secret")))
		Expect(secret.Data).To(HaveKeyWithValue("bucketName", []byte("existing-bucket")))
	})

	It("creates an all-buckets key when no bucket is referenced", func() {
		fake := &fakeB2{
			createKeyResp: &backblaze.ApplicationKeyResponse{
				KeyName:          "key-all-buckets",
				ApplicationKeyId: "app-key-id-2",
				ApplicationKey:   "app-key-secret-2",
			},
		}
		r := newKeyReconciler(fake, nil)

		key := newKey("key-all-buckets", "", "secret-key-all-buckets")
		Expect(k8sClient.Create(ctx, key)).To(Succeed())

		_, err := reconcileKey(r, "key-all-buckets")
		Expect(err).NotTo(HaveOccurred())
		_, err = reconcileKey(r, "key-all-buckets")
		Expect(err).NotTo(HaveOccurred())

		Expect(getKey("key-all-buckets").Status.Reconciled).To(BeTrue())
		Expect(fake.createdKeyNames).To(ContainElement("key-all-buckets"))
	})

	It("deletes the application key and secret when the Key is deleted", func() {
		fake := &fakeB2{
			bucket: &backblaze.Bucket{BucketInfo: &backblaze.BucketInfo{ID: "bucket-id-3", Name: "existing-bucket"}},
			createKeyResp: &backblaze.ApplicationKeyResponse{
				KeyName:          "key-delete",
				ApplicationKeyId: "app-key-id-3",
				ApplicationKey:   "app-key-secret-3",
			},
		}
		r := newKeyReconciler(fake, nil)

		key := newKey("key-delete", "existing-bucket", "secret-key-delete")
		Expect(k8sClient.Create(ctx, key)).To(Succeed())

		_, err := reconcileKey(r, "key-delete")
		Expect(err).NotTo(HaveOccurred())
		_, err = reconcileKey(r, "key-delete")
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Delete(ctx, getKey("key-delete"))).To(Succeed())
		_, err = reconcileKey(r, "key-delete")
		Expect(err).NotTo(HaveOccurred())

		Expect(fake.deletedKeyIds).To(ContainElement("app-key-id-3"))
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "key-delete", Namespace: "default"}, &b2v1alpha2.Key{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), fmt.Sprintf("expected Key to be gone, got: %v", err))
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "secret-key-delete", Namespace: "default"}, &corev1.Secret{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})
})
