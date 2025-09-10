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
    "fmt"
    "io"
    "log/slog"
    "os"
    "slices"
    "sort"
    "strings"

    "dario.cat/mergo"
    "github.com/score-spec/score-go/framework"
    scoreloader "github.com/score-spec/score-go/loader"
    scoreschema "github.com/score-spec/score-go/schema"
    scoretypes "github.com/score-spec/score-go/types"
    "github.com/spf13/cobra"
    "gopkg.in/yaml.v3"

    "github.com/score-spec/score-implementation-avassa/internal/convert"
    "github.com/score-spec/score-implementation-avassa/internal/provisioners"
    "github.com/score-spec/score-implementation-avassa/internal/state"
)

const (
    generateCmdOverridesFileFlag    = "overrides-file"
    generateCmdOverridePropertyFlag = "override-property"
    generateCmdImageFlag            = "image"
    generateCmdOutputFlag           = "output"
    generateCmdStdoutFlag           = "stdout"
)

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Run the conversion from score file to output manifests",
	Args:  cobra.ArbitraryArgs,
	CompletionOptions: cobra.CompletionOptions{
		HiddenDefaultCmd: true,
	},
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		sd, ok, err := state.LoadStateDirectory(".")
		if err != nil {
			return fmt.Errorf("failed to load existing state directory: %w", err)
		} else if !ok {
			return fmt.Errorf("state directory does not exist, please run \"init\" first")
		}
		currentState := &sd.State

		if len(args) != 1 && (cmd.Flags().Lookup(generateCmdOverridesFileFlag).Changed || cmd.Flags().Lookup(generateCmdOverridePropertyFlag).Changed || cmd.Flags().Lookup(generateCmdImageFlag).Changed) {
			return fmt.Errorf("cannot use --%s, --%s, or --%s when 0 or more than 1 score files are provided", generateCmdOverridePropertyFlag, generateCmdOverridesFileFlag, generateCmdImageFlag)
		}

		slices.Sort(args)
		for _, arg := range args {
			var rawWorkload map[string]interface{}
			if raw, err := os.ReadFile(arg); err != nil {
				return fmt.Errorf("failed to read input score file: %s: %w", arg, err)
			} else if err = yaml.Unmarshal(raw, &rawWorkload); err != nil {
				return fmt.Errorf("failed to decode input score file: %s: %w", arg, err)
			}

			// apply overrides

			if v, _ := cmd.Flags().GetString(generateCmdOverridesFileFlag); v != "" {
				if err := parseAndApplyOverrideFile(v, generateCmdOverridesFileFlag, rawWorkload); err != nil {
					return err
				}
			}

			// Now read, parse, and apply any override properties to the score files
			if v, _ := cmd.Flags().GetStringArray(generateCmdOverridePropertyFlag); len(v) > 0 {
				for _, overridePropertyEntry := range v {
					if rawWorkload, err = parseAndApplyOverrideProperty(overridePropertyEntry, generateCmdOverridePropertyFlag, rawWorkload); err != nil {
						return err
					}
				}
			}

			// Ensure transforms are applied (be a good citizen)
			if changes, err := scoreschema.ApplyCommonUpgradeTransforms(rawWorkload); err != nil {
				return fmt.Errorf("failed to upgrade spec: %w", err)
			} else if len(changes) > 0 {
				for _, change := range changes {
					slog.Info(fmt.Sprintf("Applying backwards compatible upgrade %s", change))
				}
			}

			var workload scoretypes.Workload
			if err = scoreschema.Validate(rawWorkload); err != nil {
				return fmt.Errorf("invalid score file: %s: %w", arg, err)
			} else if err = scoreloader.MapSpec(&workload, rawWorkload); err != nil {
				return fmt.Errorf("failed to decode input score file: %s: %w", arg, err)
			}

		// Apply image override
			// First validate the --image flag value if provided
			if v, _ := cmd.Flags().GetString(generateCmdImageFlag); strings.TrimSpace(v) == "." {
				return fmt.Errorf("invalid --%s value: '.' is not a valid image name; please provide an explicit image name (e.g. 'repo/name:tag')", generateCmdImageFlag)
			}

			for containerName, container := range workload.Containers {
				if container.Image == "." {
					if v, _ := cmd.Flags().GetString(generateCmdImageFlag); v != "" {
						if strings.TrimSpace(v) == "." {
							return fmt.Errorf("container '%s' has image '.'; please provide an explicit image name via --%s", containerName, generateCmdImageFlag)
						}
						container.Image = v
						slog.Info(fmt.Sprintf("Set container image for container '%s' to %s from --%s", containerName, v, generateCmdImageFlag))
						workload.Containers[containerName] = container
					} else {
						return fmt.Errorf("container '%s' has image '.'; please provide an explicit image name via --%s", containerName, generateCmdImageFlag)
					}
				}
			}

			if currentState, err = currentState.WithWorkload(&workload, &arg, state.WorkloadExtras{}); err != nil {
				return fmt.Errorf("failed to add score file to project: %s: %w", arg, err)
			}
			slog.Info("Added score file to project", "file", arg)
		}

		if len(currentState.Workloads) == 0 {
			return fmt.Errorf("project is empty, please add a score file")
		}

		if currentState, err = currentState.WithPrimedResources(); err != nil {
			return fmt.Errorf("failed to prime resources: %w", err)
		}

		slog.Info("Primed resources", "#workloads", len(currentState.Workloads), "#resources", len(currentState.Resources))

		outputManifests := make([]map[string]interface{}, 0)

		if currentState, err = provisioners.ProvisionResources(currentState); err != nil {
			return fmt.Errorf("failed to provision resources: %w", err)
		}

		sd.State = *currentState
		if err := sd.Persist(); err != nil {
			return fmt.Errorf("failed to persist state file: %w", err)
		}
		slog.Info("Persisted state file")

		for workloadName := range currentState.Workloads {
			if manifest, err := convert.Workload(currentState, workloadName); err != nil {
				return fmt.Errorf("failed to convert workloads: %w", err)
			} else {
				outputManifests = append(outputManifests, manifest)
			}
			slog.Info(fmt.Sprintf("Wrote manifest to manifests buffer for workload '%s'", workloadName))
		}

		out := new(bytes.Buffer)
        for _, manifest := range outputManifests {
            out.WriteString("---\n")
            _ = encodeManifestWithNameFirst(out, manifest)
        }
        v, _ := cmd.Flags().GetString(generateCmdOutputFlag)
        toStdout, _ := cmd.Flags().GetBool(generateCmdStdoutFlag)
        if toStdout || v == "-" {
            _, _ = fmt.Fprint(cmd.OutOrStdout(), out.String())
        } else if v == "" {
            return fmt.Errorf("no output file specified")
        } else if err := os.WriteFile(v+".tmp", out.Bytes(), 0644); err != nil {
            return fmt.Errorf("failed to write output file: %w", err)
        } else if err := os.Rename(v+".tmp", v); err != nil {
            return fmt.Errorf("failed to complete writing output file: %w", err)
        } else {
            slog.Info(fmt.Sprintf("Wrote manifests to '%s'", v))
        }
        return nil
    },
}

func parseAndApplyOverrideFile(entry string, flagName string, spec map[string]interface{}) error {
	if raw, err := os.ReadFile(entry); err != nil {
		return fmt.Errorf("--%s '%s' is invalid, failed to read file: %w", flagName, entry, err)
	} else {
		slog.Info(fmt.Sprintf("Applying overrides from %s to workload", entry))
		var out map[string]interface{}
		if err := yaml.Unmarshal(raw, &out); err != nil {
			return fmt.Errorf("--%s '%s' is invalid: failed to decode yaml: %w", flagName, entry, err)
		} else if err := mergo.Merge(&spec, out, mergo.WithOverride); err != nil {
			return fmt.Errorf("--%s '%s' failed to apply: %w", flagName, entry, err)
		}
	}
	return nil
}

func parseAndApplyOverrideProperty(entry string, flagName string, spec map[string]interface{}) (map[string]interface{}, error) {
	parts := strings.SplitN(entry, "=", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("--%s '%s' is invalid, expected a =-separated path and value", flagName, entry)
	}
	if parts[1] == "" {
		slog.Info(fmt.Sprintf("Overriding '%s' in workload", parts[0]))
		after, err := framework.OverridePathInMap(spec, framework.ParseDotPathParts(parts[0]), true, nil)
		if err != nil {
			return nil, fmt.Errorf("--%s '%s' could not be applied: %w", flagName, entry, err)
		}
		return after, nil
	} else {
		var value interface{}
		if err := yaml.Unmarshal([]byte(parts[1]), &value); err != nil {
			return nil, fmt.Errorf("--%s '%s' is invalid, failed to unmarshal value as json: %w", flagName, entry, err)
		}
		slog.Info(fmt.Sprintf("Overriding '%s' in workload", parts[0]))
		after, err := framework.OverridePathInMap(spec, framework.ParseDotPathParts(parts[0]), false, value)
		if err != nil {
			return nil, fmt.Errorf("--%s '%s' could not be applied: %w", flagName, entry, err)
		}
		return after, nil
	}
}

func init() {
    generateCmd.Flags().StringP(generateCmdOutputFlag, "o", "manifests.yaml", "The output manifests file to write the manifests to")
    generateCmd.Flags().Bool(generateCmdStdoutFlag, false, "Write the generated manifests to stdout instead of a file")
    generateCmd.Flags().String(generateCmdOverridesFileFlag, "", "An optional file of Score overrides to merge in")
    generateCmd.Flags().StringArray(generateCmdOverridePropertyFlag, []string{}, "An optional set of path=key overrides to set or remove")
    generateCmd.Flags().String(generateCmdImageFlag, "", "An optional container image to use for any container with image == '.'")
    rootCmd.AddCommand(generateCmd)
}

// encodeManifestWithNameFirst encodes the manifest as YAML ensuring that within
// any object under the "containers" list, the "name" key is emitted first.
// Other keys are emitted in lexicographical order for determinism.
func encodeManifestWithNameFirst(w io.Writer, manifest map[string]interface{}) error {
    n := toYAMLNode(manifest, "")
    enc := yaml.NewEncoder(w)
    defer enc.Close()
    return enc.Encode(n)
}

func toYAMLNode(v interface{}, parentKey string) *yaml.Node {
    switch t := v.(type) {
    case nil:
        return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
    case string:
        return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: t}
    case bool:
        if t {
            return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"}
        }
        return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"}
    case int:
        return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", t)}
    case int64:
        return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", t)}
    case float64:
        // Keep integers clean even if represented as float64
        if float64(int64(t)) == t {
            return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", int64(t))}
        }
        return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", t), "0"), ".")}
    case []interface{}:
        seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
        for _, el := range t {
            seq.Content = append(seq.Content, toYAMLNode(el, parentKey))
        }
        return seq
    case map[string]interface{}:
        // Determine key order
        keys := make([]string, 0, len(t))
        for k := range t {
            keys = append(keys, k)
        }
        // When within containers, place "name" first if present
        if parentKey == "containers" {
            sort.Strings(keys)
            // Move "name" to the front if it exists
            for i, k := range keys {
                if k == "name" {
                    if i != 0 {
                        copy(keys[1:i+1], keys[0:i])
                        keys[0] = k
                    }
                    break
                }
            }
        } else {
            sort.Strings(keys)
        }
        m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
        for _, k := range keys {
            m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k})
            m.Content = append(m.Content, toYAMLNode(t[k], k))
        }
        return m
    default:
        // Fallback: encode via yaml then decode to node (rare)
        var n yaml.Node
        if raw, err := yaml.Marshal(t); err == nil {
            _ = yaml.Unmarshal(raw, &n)
            return &n
        }
        // As a last resort, string-format
        return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: fmt.Sprintf("%v", t)}
    }
}
