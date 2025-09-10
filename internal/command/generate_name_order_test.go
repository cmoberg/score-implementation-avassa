package command

import (
    "bytes"
    "context"
    "os"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "gopkg.in/yaml.v3"
)

// Verifies that within each containers entry, the first key is always "name".
func TestGenerate_ContainersNameFirst(t *testing.T) {
    _ = changeToTempDir(t)

    // init creates a sample score.yaml with one container
    stdout, _, err := executeAndResetCommand(context.Background(), rootCmd, []string{"init"})
    require.NoError(t, err)
    assert.Equal(t, "", stdout)

    _, _, err = executeAndResetCommand(context.Background(), rootCmd, []string{
        "generate", "-o", "manifests.yaml", "--", "score.yaml",
    })
    require.NoError(t, err)

    raw, err := os.ReadFile("manifests.yaml")
    require.NoError(t, err)

    // Decode as yaml.Node to check key ordering
    var doc yaml.Node
    dec := yaml.NewDecoder(bytes.NewReader(raw))
    require.NoError(t, dec.Decode(&doc))
    require.Equal(t, yaml.DocumentNode, doc.Kind)
    require.Len(t, doc.Content, 1)
    root := doc.Content[0]
    require.Equal(t, yaml.MappingNode, root.Kind)

    // Find services -> [0] -> containers
    var servicesNode *yaml.Node
    for i := 0; i < len(root.Content); i += 2 {
        if root.Content[i].Value == "services" {
            servicesNode = root.Content[i+1]
            break
        }
    }
    if servicesNode == nil {
        t.Fatalf("services key not found")
    }
    require.Equal(t, yaml.SequenceNode, servicesNode.Kind)
    require.GreaterOrEqual(t, len(servicesNode.Content), 1)
    svc := servicesNode.Content[0]
    require.Equal(t, yaml.MappingNode, svc.Kind)

    var containersNode *yaml.Node
    for i := 0; i < len(svc.Content); i += 2 {
        if svc.Content[i].Value == "containers" {
            containersNode = svc.Content[i+1]
            break
        }
    }
    if containersNode == nil {
        t.Fatalf("containers key not found")
    }
    require.Equal(t, yaml.SequenceNode, containersNode.Kind)
    require.GreaterOrEqual(t, len(containersNode.Content), 1)

    for _, c := range containersNode.Content {
        require.Equal(t, yaml.MappingNode, c.Kind)
        // mapping nodes alternate key/value in Content; first key at index 0
        require.GreaterOrEqual(t, len(c.Content), 2)
        assert.Equal(t, "name", c.Content[0].Value, "container 'name' should be first key")
    }
}

