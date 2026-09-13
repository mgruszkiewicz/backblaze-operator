package e2e

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	backblaze "github.com/mgruszkiewicz/go-backblaze"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	b2v1alpha2 "github.com/mgruszkiewicz/backblaze-operator/api/v1alpha2"
)

// ---------------------------------------------------------------------------
// Timeouts: B2 API calls can take a while, be generous here.
// ---------------------------------------------------------------------------

const (
	eventuallyTimeout = 3 * time.Minute
	eventuallyPoll    = 10 * time.Second
)

// ---------------------------------------------------------------------------
// Naming + namespaces: every test gets its own namespace and globally
// unique bucket/key names (B2 bucket names are global).
// ---------------------------------------------------------------------------

// uniqueName returns e.g. "e2e-a1b2c3d4". B2 bucket names must be
// lowercase, 6-50 chars, letters/numbers/dashes.
func uniqueName(prefix string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	suffix := fmt.Sprintf("%x", b)
	name := strings.ToLower(fmt.Sprintf("%s-%s", prefix, suffix))
	name = strings.ReplaceAll(name, "_", "-")
	if len(name) < 6 {
		name += "-e2e00"
	}
	return name
}

// createTestNamespace creates an isolated namespace for one spec and
// registers its deletion via DeferCleanup. Returns the namespace name.
func createTestNamespace(ctx context.Context) string {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "b2-e2e-"},
	}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	DeferCleanup(func(ctx context.Context) {
		_ = k8sClient.Delete(ctx, ns)
	})
	return ns.Name
}

// ---------------------------------------------------------------------------
// Backblaze client for provider-side assertions.
// ---------------------------------------------------------------------------

// requireB2Creds Skips the current spec when B2 credentials are missing,
// so `make test` / envtest stays green without secrets.
func requireB2Creds() {
	if os.Getenv("B2_APPLICATION_ID") == "" || os.Getenv("B2_APPLICATION_KEY") == "" {
		Skip("B2_APPLICATION_ID / B2_APPLICATION_KEY not set")
	}
}

// newB2Client builds a read-only verification client from the same env
// vars the operator itself uses (see cmd/main.go).
func newB2Client() *backblaze.B2 {
	requireB2Creds()
	b2, err := backblaze.NewB2(backblaze.Credentials{
		KeyID:          os.Getenv("B2_APPLICATION_ID"),
		ApplicationKey: os.Getenv("B2_APPLICATION_KEY"),
	})
	Expect(err).NotTo(HaveOccurred())
	return b2
}

// ---------------------------------------------------------------------------
// K8s-side wait helpers.
// ---------------------------------------------------------------------------

func getBucket(ctx context.Context, namespace, name string) *b2v1alpha2.Bucket {
	obj := &b2v1alpha2.Bucket{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj)).To(Succeed())
	return obj
}

func getKey(ctx context.Context, namespace, name string) *b2v1alpha2.Key {
	obj := &b2v1alpha2.Key{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj)).To(Succeed())
	return obj
}

func isReadyTrue(conditions []metav1.Condition) bool {
	cond := meta.FindStatusCondition(conditions, "Ready")
	return cond != nil && cond.Status == metav1.ConditionTrue
}

// waitForBucketReady polls until .status.conditions[Ready=True] and
// .status.reconciled=true.
func waitForBucketReady(ctx context.Context, namespace, name string) *b2v1alpha2.Bucket {
	var obj *b2v1alpha2.Bucket
	Eventually(func(g Gomega) {
		obj = &b2v1alpha2.Bucket{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj)).To(Succeed())
		g.Expect(obj.Status.Reconciled).To(BeTrue())
		g.Expect(isReadyTrue(obj.Status.Conditions)).To(BeTrue())
	}, eventuallyTimeout, eventuallyPoll).Should(Succeed())
	return obj
}

// waitForKeyReady polls until the Key reports Ready and has a keyId.
func waitForKeyReady(ctx context.Context, namespace, name string) *b2v1alpha2.Key {
	var obj *b2v1alpha2.Key
	Eventually(func(g Gomega) {
		obj = &b2v1alpha2.Key{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj)).To(Succeed())
		g.Expect(obj.Status.Reconciled).To(BeTrue())
		g.Expect(isReadyTrue(obj.Status.Conditions)).To(BeTrue())
	}, eventuallyTimeout, eventuallyPoll).Should(Succeed())
	return obj
}

// waitForDeletion polls until the object is gone (finalizer removed).
func waitForDeletion(ctx context.Context, obj client.Object) {
	Eventually(func(g Gomega) {
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()}, obj)
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue())
	}, eventuallyTimeout, eventuallyPoll).Should(Succeed())
}

func getSecret(ctx context.Context, namespace, name string) *corev1.Secret {
	s := &corev1.Secret{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, s)).To(Succeed())
	return s
}

// namespacedName is a tiny shortcut for lookups in cleanup blocks.
func namespacedName(namespace, name string) types.NamespacedName {
	return types.NamespacedName{Namespace: namespace, Name: name}
}

// clientObject / namespacedObject support generic cleanup loops
// (delete Key then Bucket) without repeating Get/Delete boilerplate.
type clientObject = client.Object

type namespacedObject struct {
	namespace string
	name      string
	newObj    func() clientObject
}
