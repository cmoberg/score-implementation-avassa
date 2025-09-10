// Copyright 2024 Humanitec
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package command

import (
    "bytes"
    "context"
    "os"
    "path/filepath"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "gopkg.in/yaml.v3"

    "github.com/score-spec/score-implementation-avassa/internal/state"
)

func changeToDir(t *testing.T, dir string) string {
	t.Helper()
	wd, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(wd))
	})
	return dir
}

func changeToTempDir(t *testing.T) string {
	return changeToDir(t, t.TempDir())
}

func TestGenerateWithoutInit(t *testing.T) {
	_ = changeToTempDir(t)
	stdout, _, err := executeAndResetCommand(context.Background(), rootCmd, []string{"generate"})
	assert.EqualError(t, err, "state directory does not exist, please run \"init\" first")
	assert.Equal(t, "", stdout)
}

func TestGenerateWithoutScoreFiles(t *testing.T) {
	_ = changeToTempDir(t)
	stdout, _, err := executeAndResetCommand(context.Background(), rootCmd, []string{"init"})
	assert.NoError(t, err)
	assert.Equal(t, "", stdout)
	stdout, _, err = executeAndResetCommand(context.Background(), rootCmd, []string{"generate"})
	assert.EqualError(t, err, "project is empty, please add a score file")
	assert.Equal(t, "", stdout)
}

func TestInitAndGenerateWithBadFile(t *testing.T) {
	td := changeToTempDir(t)
	stdout, _, err := executeAndResetCommand(context.Background(), rootCmd, []string{"init"})
	assert.NoError(t, err)
	assert.Equal(t, "", stdout)

	assert.NoError(t, os.WriteFile(filepath.Join(td, "thing"), []byte(`"blah"`), 0644))

	stdout, _, err = executeAndResetCommand(context.Background(), rootCmd, []string{"generate", "thing"})
	assert.EqualError(t, err, "failed to decode input score file: thing: yaml: unmarshal errors:\n  line 1: cannot unmarshal !!str `blah` into map[string]interface {}")
	assert.Equal(t, "", stdout)
}

func TestInitAndGenerateWithBadScore(t *testing.T) {
	td := changeToTempDir(t)
	stdout, _, err := executeAndResetCommand(context.Background(), rootCmd, []string{"init"})
	assert.NoError(t, err)
	assert.Equal(t, "", stdout)

	assert.NoError(t, os.WriteFile(filepath.Join(td, "thing"), []byte(`{}`), 0644))

	stdout, _, err = executeAndResetCommand(context.Background(), rootCmd, []string{"generate", "thing"})
	assert.EqualError(t, err, "invalid score file: thing: jsonschema: '' does not validate with https://score.dev/schemas/score#/required: missing properties: 'apiVersion', 'metadata', 'containers'")
	assert.Equal(t, "", stdout)
}

func TestInitAndGenerate_with_sample(t *testing.T) {
    td := changeToTempDir(t)
    stdout, _, err := executeAndResetCommand(context.Background(), rootCmd, []string{"init"})
    require.NoError(t, err)
    assert.Equal(t, "", stdout)
    stdout, _, err = executeAndResetCommand(context.Background(), rootCmd, []string{
        "generate", "-o", "manifests.yaml", "--", "score.yaml",
    })
    require.NoError(t, err)
    assert.Equal(t, "", stdout)
    raw, err := os.ReadFile(filepath.Join(td, "manifests.yaml"))
    assert.NoError(t, err)

    var doc map[string]interface{}
    dec := yaml.NewDecoder(bytes.NewReader(raw))
    assert.NoError(t, dec.Decode(&doc))

    assert.Equal(t, "example", doc["name"]) // application name
    assert.Equal(t, "restart-service-instance", doc["on-mutable-variable-change"]) // default

    services, ok := doc["services"].([]interface{})
    if assert.True(t, ok) && assert.Len(t, services, 1) {
        svc := services[0].(map[string]interface{})
        assert.Equal(t, "example-service", svc["name"]) 
        assert.Equal(t, "replicated", svc["mode"]) 
        // replicas comes back as int or float64 depending on YAML lib
        switch v := svc["replicas"].(type) {
        case int:
            assert.Equal(t, 1, v)
        case int64:
            assert.Equal(t, int64(1), v)
        case float64:
            assert.Equal(t, 1.0, v)
        default:
            t.Fatalf("unexpected replicas type: %T", v)
        }

        containers, ok := svc["containers"].([]interface{})
        if assert.True(t, ok) && assert.Len(t, containers, 1) {
            c0 := containers[0].(map[string]interface{})
            assert.Equal(t, "main", c0["name"])
            assert.Equal(t, "stefanprodan/podinfo", c0["image"])
            // mounts empty slice
            if m, ok := c0["mounts"].([]interface{}); ok {
                assert.Len(t, m, 0)
            } else {
                t.Fatalf("mounts missing or wrong type")
            }
        }
    }

    // check that state was persisted
    sd, ok, err := state.LoadStateDirectory(td)
    assert.NoError(t, err)
    assert.True(t, ok)
	assert.Equal(t, "score.yaml", *sd.State.Workloads["example"].File)
	assert.Len(t, sd.State.Workloads, 1)
	assert.Len(t, sd.State.Resources, 0)
}

func TestInitAndGenerate_with_full_example(t *testing.T) {
    td := changeToTempDir(t)
    stdout, _, err := executeAndResetCommand(context.Background(), rootCmd, []string{"init"})
    require.NoError(t, err)
    assert.Equal(t, "", stdout)

	_ = os.Remove(filepath.Join(td, "score.yaml"))
	assert.NoError(t, os.WriteFile(filepath.Join(td, "score.yaml"), []byte(`
apiVersion: score.dev/v1b1
metadata:
    name: example
containers:
    main:
        image: stefanprodan/podinfo
        variables:
            key: value
            dynamic: ${metadata.name}
        files:
        - target: /somefile
          content: |
            ${metadata.name}
resources:
    thing:
        type: something
        params:
          x: ${metadata.name}
`), 0755))

    _, _, err = executeAndResetCommand(context.Background(), rootCmd, []string{
        "generate", "-o", "manifests.yaml", "--", "score.yaml",
    })
    require.NoError(t, err)
    raw, err := os.ReadFile(filepath.Join(td, "manifests.yaml"))
    assert.NoError(t, err)

    var doc map[string]interface{}
    dec := yaml.NewDecoder(bytes.NewReader(raw))
    assert.NoError(t, dec.Decode(&doc))

    assert.Equal(t, "example", doc["name"]) // application name
    services, ok := doc["services"].([]interface{})
    if assert.True(t, ok) && assert.Len(t, services, 1) {
        svc := services[0].(map[string]interface{})
        containers, ok := svc["containers"].([]interface{})
        if assert.True(t, ok) && assert.Len(t, containers, 1) {
            c0 := containers[0].(map[string]interface{})
            assert.Equal(t, "stefanprodan/podinfo", c0["image"])
            // env should contain interpolated variables
            if env, ok := c0["env"].(map[string]interface{}); ok {
                assert.Equal(t, "value", env["key"])           // literal
                assert.Equal(t, "example", env["dynamic"])      // ${metadata.name} -> example
            } else {
                t.Fatalf("env missing or wrong type")
            }
        }
    }
}
