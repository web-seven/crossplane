/*
Copyright 2022 The Crossplane Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/e2e-framework/klient/conf"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/pkg/features"
	"sigs.k8s.io/e2e-framework/third_party/helm"

	"github.com/crossplane/crossplane/test/e2e/config"
	"github.com/crossplane/crossplane/test/e2e/funcs"
)

// TODO(phisco): make it configurable
const namespace = "crossplane-system"

// TODO(phisco): make it configurable
const crdsDir = "cluster/crds"

// The caller (e.g. make e2e) must ensure these exist.
// Run `make build e2e-tag-images` to produce them
const (
	// TODO(phisco): make it configurable
	imgcore = "crossplane-e2e/crossplane:latest"
)

const (
	// TODO(phisco): make it configurable
	helmChartDir = "cluster/charts/crossplane"
	// TODO(phisco): make it configurable
	helmReleaseName = "crossplane"
)

var (
	e2eConfig   = config.NewFromFlags()
	clusterName string
)

func TestMain(m *testing.M) {
	// TODO(negz): Global loggers are dumb and klog is dumb. Remove this when
	// e2e-framework is running controller-runtime v0.15.x per
	// https://github.com/kubernetes-sigs/e2e-framework/issues/270
	log.SetLogger(klog.NewKlogr())

	// Set the default suite, to be used as base for all the other suites.
	e2eConfig.AddDefaultTestSuite(
		config.WithoutBaseDefaultTestSuite(),
		config.WithHelmInstallOpts(
			helm.WithName(helmReleaseName),
			helm.WithNamespace(namespace),
			helm.WithChart(helmChartDir),
			// wait for the deployment to be ready for up to 5 minutes before returning
			helm.WithWait(),
			helm.WithTimeout("5m"),
			helm.WithArgs(
				// Run with debug logging to ensure all log statements are run.
				"--set args={--debug}",
				"--set image.repository="+strings.Split(imgcore, ":")[0],
				"--set image.tag="+strings.Split(imgcore, ":")[1],
			),
		),
		config.WithLabelsToSelect(features.Labels{
			config.LabelTestSuite: []string{config.TestSuiteDefault},
		}),
	)

	cfg, err := envconf.NewFromFlags()
	if err != nil {
		panic(err)
	}

	var setup []env.Func
	var finish []env.Func

	// Parse flags, populating Config too.
	// we want to create the cluster if it doesn't exist, but only if we're
	if e2eConfig.IsKindCluster() {
		clusterName := e2eConfig.GetKindClusterName()
		kindCfg, err := filepath.Abs(filepath.Join("test", "e2e", "testdata", "kindConfig.yaml"))
		if err != nil {
			panic(fmt.Sprintf("error getting kind config file: %s", err.Error()))
		}
		setup = []env.Func{
			funcs.CreateKindClusterWithConfig(clusterName, kindCfg),
		}
	} else {
		cfg.WithKubeconfigFile(conf.ResolveKubeConfigFile())
	}

	// Enrich the selected labels with the ones from the suite.
	// Not replacing the user provided ones if any.
	cfg.WithLabels(e2eConfig.EnrichLabels(cfg.Labels()))

	e2eConfig.SetEnvironment(env.NewWithConfig(cfg))

	if e2eConfig.ShouldLoadImages() {
		clusterName := e2eConfig.GetKindClusterName()
		setup = append(setup,
			envfuncs.LoadDockerImageToCluster(clusterName, imgcore),
		)
	}

	// Add the setup functions defined by the suite being used
	setup = append(setup,
		e2eConfig.GetSelectedSuiteAdditionalEnvSetup()...,
	)

	if e2eConfig.ShouldInstallCrossplane() {
		setup = append(setup,
			envfuncs.CreateNamespace(namespace),
			e2eConfig.HelmInstallBaseCrossplane(),
		)
	}

	// We always want to add our types to the scheme.
	setup = append(setup, funcs.AddCrossplaneTypesToScheme())

	// We want to destroy the cluster if we created it, but only if we created it,
	// otherwise the random name will be meaningless.
	if e2eConfig.ShouldDestroyKindCluster() {
		finish = []env.Func{envfuncs.DestroyKindCluster(e2eConfig.GetKindClusterName())}
	}

	// Check that all features are specifying a suite they belong to via LabelTestSuite.
	e2eConfig.BeforeEachFeature(func(ctx context.Context, _ *envconf.Config, t *testing.T, feature features.Feature) (context.Context, error) {
		if _, exists := feature.Labels()[config.LabelTestSuite]; !exists {
			t.Fatalf("Feature %q does not have the required %q label set", feature.Name(), config.LabelTestSuite)
		}
		return ctx, nil
	})

	e2eConfig.Setup(setup...)
	e2eConfig.Finish(finish...)
	os.Exit(e2eConfig.Run(m))
}
