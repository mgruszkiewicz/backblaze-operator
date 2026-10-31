## Contributing
Any contribution, tips and tricks are highly apperated

### Test It Out
1. Install the CRDs into the cluster:

```sh
make install
```

2. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
make run
```

**NOTE:** You can also run this in one step by running: `make install run`

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)
## Updating the Helm chart

The Helm chart lives in [mgruszkiewicz/helm-charts](https://github.com/mgruszkiewicz/helm-charts) and is generated
from the kustomize manifests with [helmify](https://github.com/arttor/helmify). Check out `helm-charts` next to this
repository (or set `HELM_CHART_DIR`) and run:

```sh
make helm                       # regenerate ../helm-charts/charts/backblaze-operator
make helm HELM_CHART_DIR=/path/to/charts/backblaze-operator
```

helmify only knows about the raw manifests, so manual chart customisations (existing-secret support, documented
values, ...) are stored in `hack/helm/chart.patch` and re-applied on every run. `Chart.yaml`, `README.md`,
`artifacthub-pkg.yml`, `.helmignore` and `templates/_helpers.tpl` are hand-maintained and never touched.
If the patch no longer applies, `make helm` fails without modifying the chart.

To add a new manual customisation:

1. `make helm` so the chart matches the current manifests,
2. edit the chart in `helm-charts` by hand,
3. `make helm-patch` to record the edit in `hack/helm/chart.patch`, and commit it.

Remember to bump the chart `version` (and `appVersion` when releasing a new operator version) in `Chart.yaml`.
