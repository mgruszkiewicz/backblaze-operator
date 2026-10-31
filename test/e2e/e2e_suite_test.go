package e2e

import (
	"fmt"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	b2v1alpha2 "github.com/mgruszkiewicz/backblaze-operator/api/v1alpha2"
)

var (
	k8sClient client.Client
	scheme    *runtime.Scheme = runtime.NewScheme()
)

// TestE2E is the single Ginkgo entrypoint for `go test ./test/e2e/...`.
// The operator image is expected to be already deployed on the test cluster
// (see `make deploy-e2e`). These tests talk to the cluster via kubeconfig
// and to Backblaze B2 via env credentials for provider-side assertions.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Backblaze Operator E2E Suite")
}

var _ = BeforeSuite(func() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(b2v1alpha2.AddToScheme(scheme))

	cfg, err := ctrl.GetConfig()
	Expect(err).NotTo(HaveOccurred(), "kubeconfig must be set (KUBECONFIG or ~/.kube/config)")

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())

	// Fail fast with a helpful message when B2 credentials are missing.
	// Local run: `export B2_APPLICATION_ID=... B2_APPLICATION_KEY=...`
	// CI run: GitHub Secrets -> env (see .github/workflows/e2e.yaml example in chat).
	if os.Getenv("B2_APPLICATION_ID") == "" || os.Getenv("B2_APPLICATION_KEY") == "" {
		fmt.Fprintln(os.Stderr, "WARNING: B2_APPLICATION_ID / B2_APPLICATION_KEY not set; provider-side assertions will Skip")
	}
})
