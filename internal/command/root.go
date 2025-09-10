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
    "io"
    "log/slog"

    "github.com/spf13/cobra"
    "github.com/score-spec/score-implementation-avassa/internal/version"
)

var verbose bool

var rootCmd = &cobra.Command{
    Use:           "score-implementation-avassa",
	SilenceErrors: true,
	CompletionOptions: cobra.CompletionOptions{
		HiddenDefaultCmd: true,
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var h slog.Handler
		if verbose {
			h = slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: slog.LevelInfo})
		} else {
			h = slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})
		}
		slog.SetDefault(slog.New(h))
		return nil
	},
}

func init() {
	rootCmd.Version = version.BuildVersionString()
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging to stderr")
}

func Execute() error {
	return rootCmd.Execute()
}
