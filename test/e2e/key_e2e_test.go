package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	b2v1alpha2 "github.com/mgruszkiewicz/backblaze-operator/api/v1alpha2"
)

// Key E2E: creates a Bucket + Key, checks the connection Secret and the
// provider-side key permissions. Fill in the TODOs for your capabilities.
var _ = Describe("Key E2E", Label("key", "e2e"), func() {
	var ctx context.Context
	var namespace string

	BeforeEach(func() {
		ctx = context.Background()
		namespace = createTestNamespace(ctx)
	})

	It("creates a key restricted to one bucket and writes the connection secret", func() {
		requireB2Creds()
		b2 := newB2Client()
		bucketName := uniqueName("e2e-bkt")
		keyName := uniqueName("e2e-key")
		secretName := keyName + "-conn"

		// Prerequisite bucket (Key with bucketName waits on it).
		Expect(k8sClient.Create(ctx, &b2v1alpha2.Bucket{
			ObjectMeta: metav1.ObjectMeta{Name: bucketName, Namespace: namespace},
			Spec: b2v1alpha2.BucketSpec{
				AtProvider: b2v1alpha2.BucketSpecAtProvider{Acl: "private"},
			},
		})).To(Succeed())
		waitForBucketReady(ctx, namespace, bucketName)

		key := &b2v1alpha2.Key{
			ObjectMeta: metav1.ObjectMeta{Name: keyName, Namespace: namespace},
			Spec: b2v1alpha2.KeySpec{
				AtProvider: b2v1alpha2.KeySpecAtProvider{
					BucketName:   bucketName,
					Capabilities: []string{"listFiles", "readFiles"},
				},
				WriteConnectionSecretToRef: b2v1alpha2.WriteConnectionSecretToRef{Name: secretName},
			},
		}
		Expect(k8sClient.Create(ctx, key)).To(Succeed())

		DeferCleanup(func(ctx context.Context) {
			for _, obj := range []namespacedObject{
				{namespace: namespace, name: keyName, newObj: func() clientObject { return &b2v1alpha2.Key{} }},
				{namespace: namespace, name: bucketName, newObj: func() clientObject { return &b2v1alpha2.Bucket{} }},
			} {
				o := obj.newObj()
				if err := k8sClient.Get(ctx, namespacedName(obj.namespace, obj.name), o); err == nil {
					_ = k8sClient.Delete(ctx, o)
				}
			}
		})

		// 1. K8s-side: Key Ready + Secret written.
		got := waitForKeyReady(ctx, namespace, keyName)
		Expect(got.Status.KeyId).NotTo(BeEmpty())
		secret := getSecret(ctx, namespace, secretName)
		Expect(secret.Data).To(HaveKey("AWS_ACCESS_KEY_ID"))
		Expect(secret.Data).To(HaveKey("AWS_SECRET_ACCESS_KEY"))

		// 2. Provider-side: key really exists with correct capabilities/scope.
		// TODO(you): assert via B2 API, e.g. list keys and find got.Status.KeyId,
		//   then check capabilities == ["listFiles","readFiles"] and bucketId matches.
		//   Hint: b2 client in helpers.go; key_controller_test.go shows the
		//   expected Secret keys (AWS_ACCESS_KEY_ID / bucketName).
		_ = b2 // remove once you use it above

		// 3. TODO(you): delete path — Delete the Key, assert the Secret is gone
		//    (waitForDeletion on Secret) and the provider key is revoked.
	})
})
