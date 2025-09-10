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

package convert

import (
    "fmt"
    "maps"
    "os"
    "path/filepath"
    "regexp"
    "sort"
    "strconv"
    "strings"

    "github.com/score-spec/score-go/framework"
    scoretypes "github.com/score-spec/score-go/types"
    "gopkg.in/yaml.v3"

    "github.com/score-spec/score-implementation-avassa/internal/state"
)

func Workload(currentState *state.State, workloadName string) (map[string]interface{}, error) {
    resOutputs, err := currentState.GetResourceOutputForWorkload(workloadName)
    if err != nil {
        return nil, fmt.Errorf("failed to generate outputs: %w", err)
    }
    sf := framework.BuildSubstitutionFunction(currentState.Workloads[workloadName].Spec.Metadata, resOutputs)

    spec := currentState.Workloads[workloadName].Spec
    containers := maps.Clone(spec.Containers)
    for containerName, container := range containers {
        if container.Variables, err = convertContainerVariables(container.Variables, sf); err != nil {
            return nil, fmt.Errorf("workload: %s: container: %s: variables: %w", workloadName, containerName, err)
        }
        if container.Files, err = convertContainerFiles(container.Files, currentState.Workloads[workloadName].File, sf); err != nil {
            return nil, fmt.Errorf("workload: %s: container: %s: files: %w", workloadName, containerName, err)
        }
        containers[containerName] = container
    }

    // Build Avassa Application spec (subset)
    app, err := buildAvassaApplication(spec.Metadata, workloadName, containers, sf)
    if err != nil {
        return nil, err
    }

    // Marshal to YAML then back to map[string]interface{} for downstream pipeline
    raw, err := yaml.Marshal(app)
    if err != nil {
        return nil, fmt.Errorf("workload: %s: failed to serialise avassa manifest: %w", workloadName, err)
    }
    var out map[string]interface{}
    if err := yaml.Unmarshal(raw, &out); err != nil {
        return nil, fmt.Errorf("workload: %s: failed to deserialise avassa manifest: %w", workloadName, err)
    }
    return out, nil
}

func convertContainerVariables(input scoretypes.ContainerVariables, sf func(string) (string, error)) (map[string]string, error) {
	outMap := make(map[string]string, len(input))
	for key, value := range input {
		out, err := framework.SubstituteString(value, sf)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		outMap[key] = out
	}
	return outMap, nil
}

func convertContainerFiles(input map[string]scoretypes.ContainerFile, scoreFile *string, sf func(string) (string, error)) (map[string]scoretypes.ContainerFile, error) {
	output := make(map[string]scoretypes.ContainerFile, len(input))
	for target, file := range input {
		var content string
		if file.Content != nil {
			content = *file.Content
		} else if file.Source != nil {
			sourcePath := *file.Source
			if !filepath.IsAbs(sourcePath) && scoreFile != nil {
				sourcePath = filepath.Join(filepath.Dir(*scoreFile), sourcePath)
			}
			if rawContent, err := os.ReadFile(sourcePath); err != nil {
				return nil, fmt.Errorf("%s: source: failed to read file '%s': %w", target, sourcePath, err)
			} else {
				content = string(rawContent)
			}
		} else {
			return nil, fmt.Errorf("%s: missing 'content' or 'source'", target)
		}

		var err error
		if file.NoExpand == nil || !*file.NoExpand {
			content, err = framework.SubstituteString(string(content), sf)
			if err != nil {
				return nil, fmt.Errorf("%s: failed to substitute in content: %w", target, err)
			}
		}
		file.Source = nil
		file.Content = &content
		bTrue := true
		file.NoExpand = &bTrue
		output[target] = file
	}
	return output, nil
}

// ========================= Avassa helpers and types =========================

type avassaApplication struct {
    Name                      string            `yaml:"name"`
    Version                   string            `yaml:"version,omitempty"`
    Services                  []avassaService   `yaml:"services"`
    OnMutableVariableChange   string            `yaml:"on-mutable-variable-change,omitempty"`
    Labels                    map[string]any    `yaml:"labels,omitempty"`
    Network                   *avassaNetwork    `yaml:"network,omitempty"`
}

type avassaNetwork struct {
    SharedApplicationNetwork string `yaml:"shared-application-network"`
}

type avassaService struct {
    Name               string             `yaml:"name"`
    Mode               string             `yaml:"mode"`
    Replicas           int                `yaml:"replicas"`
    SharePidNamespace  bool               `yaml:"share-pid-namespace"`
    Containers         []avassaContainer  `yaml:"containers"`
}

type avassaOnMountedFileChange struct {
    Restart bool `yaml:"restart"`
}

type avassaContainer struct {
    Name                 string                        `yaml:"name"`
    Mounts               []any                         `yaml:"mounts"`
    ContainerLogSize     string                        `yaml:"container-log-size,omitempty"`
    ContainerLogArchive  bool                          `yaml:"container-log-archive,omitempty"`
    ShutdownTimeout      string                        `yaml:"shutdown-timeout,omitempty"`
    Image                string                        `yaml:"image"`
    Cmd                  []string                      `yaml:"cmd,omitempty"`
    Env                  map[string]string             `yaml:"env,omitempty"`
    Approle              string                        `yaml:"approle,omitempty"`
    OnMountedFileChange  *avassaOnMountedFileChange    `yaml:"on-mounted-file-change,omitempty"`
    Probes               *avassaProbes                 `yaml:"probes,omitempty"`
}

type avassaProbes struct {
    Liveness  *avassaProbeSpec `yaml:"liveness,omitempty"`
    Readiness *avassaProbeSpec `yaml:"readiness,omitempty"`
}

type avassaProbeSpec struct {
    HTTP             *avassaHTTPProbe `yaml:"http,omitempty"`
    TCP              *avassaTCPProbe  `yaml:"tcp,omitempty"`
    Exec             *avassaExecProbe `yaml:"exec,omitempty"`
    InitialDelay     string           `yaml:"initial-delay,omitempty"`
    Timeout          string           `yaml:"timeout,omitempty"`
    Period           string           `yaml:"period,omitempty"`
    SuccessThreshold int              `yaml:"success-threshold,omitempty"`
    FailureThreshold int              `yaml:"failure-threshold,omitempty"`
}

type avassaHTTPProbe struct {
    Scheme         string            `yaml:"scheme,omitempty"`
    Host           string            `yaml:"host,omitempty"`
    Path           string            `yaml:"path"`
    Port           int               `yaml:"port"`
    RequestHeaders map[string]string `yaml:"request-headers,omitempty"`
}

type avassaTCPProbe struct {
    Port int `yaml:"port"`
}

type avassaExecProbe struct {
    Cmd []string `yaml:"cmd"`
}

func buildAvassaApplication(metadata map[string]interface{}, workloadName string, containers map[string]scoretypes.Container, sf func(string) (string, error)) (avassaApplication, error) {
    // Name
    appName := sanitizeName(asString(metadata["name"]))
    if appName == "" {
        appName = sanitizeName(workloadName)
    }

    // Annotations (kebab-case under metadata.annotations)
    annotations := map[string]interface{}{}
    if rawAnn, ok := metadata["annotations"].(map[string]interface{}); ok {
        annotations = rawAnn
    }

    // Top-level fields
    app := avassaApplication{Name: appName}
    if v := asString(annotations["avassa.on-mutable-variable-change"]); v != "" {
        app.OnMutableVariableChange = v
    } else {
        app.OnMutableVariableChange = "restart-service-instance"
    }

    if labels, ok := metadata["labels"].(map[string]interface{}); ok && len(labels) > 0 {
        app.Labels = labels
    }
    if v := asString(annotations["avassa.network"]); v != "" {
        app.Network = &avassaNetwork{SharedApplicationNetwork: v}
    }
    if v := asString(annotations["avassa.io/version"]); strings.TrimSpace(v) != "" {
        app.Version = strings.TrimSpace(v)
    }

    // Service
    svc := avassaService{
        Name:              fmt.Sprintf("%s-service", appName),
        Mode:              "replicated",
        Replicas:          asInt(annotations["avassa.replicas"], 1),
        SharePidNamespace: asBool(annotations["avassa.share-pid-namespace"], false),
    }

    // Containers (deterministic order)
    names := make([]string, 0, len(containers))
    for n := range containers {
        names = append(names, n)
    }
    sort.Strings(names)
    for _, cname := range names {
        c := containers[cname]
        env := map[string]string{}
        for k, v := range c.Variables {
            env[k] = v
        }
        var onMnt *avassaOnMountedFileChange
        if asBool(annotations["avassa.on-mounted-file-change-restart"], false) {
            onMnt = &avassaOnMountedFileChange{Restart: true}
        }
        ac := avassaContainer{
            Name:                cname,
            Mounts:              []any{},
            ContainerLogSize:    firstNonEmpty(asString(annotations["avassa.log-size"]), "100 MB"),
            ContainerLogArchive: asBool(annotations["avassa.log-archive"], false),
            ShutdownTimeout:     firstNonEmpty(asString(annotations["avassa.shutdown-timeout"]), "10s"),
            Image:               c.Image,
            Env:                 env,
            OnMountedFileChange: onMnt,
        }
        // Map Score command/args -> Avassa cmd
        if n := len(c.Command) + len(c.Args); n > 0 {
            combined := make([]string, 0, n)
            combined = append(combined, c.Command...)
            combined = append(combined, c.Args...)
            cmdOut := make([]string, 0, len(combined))
            for _, part := range combined {
                p := strings.TrimSpace(part)
                if p == "" { continue }
                if sf != nil {
                    if subst, err := framework.SubstituteString(p, sf); err == nil {
                        p = subst
                    } else {
                        return avassaApplication{}, fmt.Errorf("workload: %s: container: %s: cmd: %w", workloadName, cname, err)
                    }
                }
                if t := strings.TrimSpace(p); t != "" {
                    cmdOut = append(cmdOut, t)
                }
            }
            if len(cmdOut) > 0 {
                ac.Cmd = cmdOut
            }
        }
        if v := asString(annotations["avassa.approle"]); v != "" {
            ac.Approle = v
        }

        // Omit env if empty to reduce noise
        if len(ac.Env) == 0 {
            ac.Env = nil
        }

        // Probes (map Score -> Avassa). Prefer exec if both present.
        var probes avassaProbes
        if p := mapScoreProbeToAvassa(c.LivenessProbe); p != nil {
            probes.Liveness = p
        }
        if p := mapScoreProbeToAvassa(c.ReadinessProbe); p != nil {
            probes.Readiness = p
        }
        if probes.Liveness != nil || probes.Readiness != nil {
            ac.Probes = &probes
        }
        svc.Containers = append(svc.Containers, ac)
    }
    app.Services = []avassaService{svc}
    return app, nil
}

var validNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]*[a-z0-9])?$`)

func sanitizeName(in string) string {
    in = strings.TrimSpace(strings.ToLower(in))
    if in == "" {
        return in
    }
    // replace invalid chars with '-'
    out := make([]rune, 0, len(in))
    for _, r := range in {
        if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
            out = append(out, r)
        } else {
            out = append(out, '-')
        }
    }
    s := strings.Trim(outStr(out), "-")
    if s == "" {
        return "app"
    }
    if !validNameRe.MatchString(s) {
        // collapse multiple dashes
        s = strings.Trim(strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == '-' }), "-"), "-")
        if s == "" {
            s = "app"
        }
    }
    return s
}

func outStr(r []rune) string { return string(r) }

func asString(v interface{}) string {
    switch t := v.(type) {
    case string:
        return t
    case fmt.Stringer:
        return t.String()
    case int:
        return strconv.Itoa(t)
    case int64:
        return strconv.FormatInt(t, 10)
    case float64:
        // JSON numbers become float64; keep integers clean
        if t == float64(int64(t)) {
            return strconv.FormatInt(int64(t), 10)
        }
        return strconv.FormatFloat(t, 'f', -1, 64)
    case bool:
        if t { return "true" }
        return "false"
    default:
        return ""
    }
}

func asInt(v interface{}, def int) int {
    switch t := v.(type) {
    case int:
        return t
    case int64:
        return int(t)
    case float64:
        return int(t)
    case string:
        if i, err := strconv.Atoi(strings.TrimSpace(t)); err == nil { return i }
        return def
    default:
        return def
    }
}

func asBool(v interface{}, def bool) bool {
    switch t := v.(type) {
    case bool:
        return t
    case string:
        if t == "true" { return true }
        if t == "false" { return false }
        return def
    default:
        return def
    }
}

func firstNonEmpty(values ...string) string {
    for _, v := range values {
        if strings.TrimSpace(v) != "" {
            return v
        }
    }
    return ""
}

// mapScoreProbeToAvassa converts a Score probe (HTTP or Exec) to an Avassa probe spec.
// If both Exec and HTTP are present, Exec is preferred.
func mapScoreProbeToAvassa(p *scoretypes.ContainerProbe) *avassaProbeSpec {
    if p == nil { return nil }
    out := &avassaProbeSpec{}
    if p.Exec != nil && len(p.Exec.Command) > 0 {
        out.Exec = &avassaExecProbe{Cmd: append([]string{}, p.Exec.Command...)}
        return out
    }
    if p.HttpGet != nil {
        http := &avassaHTTPProbe{Path: p.HttpGet.Path, Port: p.HttpGet.Port}
        if p.HttpGet.Scheme != nil {
            // Score scheme is enum HTTP|HTTPS; Avassa expects lowercase http|https
            http.Scheme = strings.ToLower(string(*p.HttpGet.Scheme))
        }
        if p.HttpGet.Host != nil {
            http.Host = *p.HttpGet.Host
        }
        if len(p.HttpGet.HttpHeaders) > 0 {
            headers := make(map[string]string, len(p.HttpGet.HttpHeaders))
            for _, h := range p.HttpGet.HttpHeaders {
                name := strings.TrimSpace(h.Name)
                val := strings.TrimSpace(h.Value)
                if name == "" { continue }
                if prev, ok := headers[name]; ok && prev != "" {
                    headers[name] = prev + ", " + val
                } else {
                    headers[name] = val
                }
            }
            if len(headers) > 0 {
                http.RequestHeaders = headers
            }
        }
        out.HTTP = http
        return out
    }
    return nil
}
