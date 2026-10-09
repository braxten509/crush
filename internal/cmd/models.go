package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"

	"charm.land/catwalk/pkg/catwalk"
	"charm.land/lipgloss/v2/tree"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "List all available models from known providers",
	Long:  `List all available models from known providers. Shows provider name and model IDs. Unconfigured providers are marked with (not configured).`,
	Example: `# List all available models
crush models

# Search models
crush models gpt5

# Usable models with their effort levels and fast mode, as JSON
crush models --json`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := ResolveCwd(cmd)
		if err != nil {
			return err
		}

		dataDir, _ := cmd.Flags().GetString("data-dir")
		debug, _ := cmd.Flags().GetBool("debug")
		if refresh, _ := cmd.Flags().GetBool("refresh"); refresh {
			config.RefreshCLIModels()
		}

		cfg, err := config.Init(cwd, dataDir, debug)
		if err != nil {
			return err
		}

		term := strings.ToLower(strings.Join(args, " "))
		if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
			return printModelsJSON(cmd.OutOrStdout(), cfg.Config(), term)
		}

		type providerEntry struct {
			name       string
			models     []string
			configured bool
		}

		entries := make(map[string]*providerEntry)

		// Add configured providers first.
		for providerID, provider := range cfg.Config().Providers.Seq2() {
			if provider.Disable {
				continue
			}
			entry := &providerEntry{
				name:       provider.Name,
				configured: true,
			}

			// The OpenAI and xAI providers each hold exactly one
			// credential. Signed in with ChatGPT or Grok, only the
			// models the subscription grants are usable; an API key
			// lists the regular catalog.
			var models []catwalk.Model
			switch {
			case providerID == string(catwalk.InferenceProviderOpenAI) && provider.OAuthToken != nil:
				models = provider.ChatGPTModels
			case providerID == string(catwalk.InferenceProviderXAI) && provider.OAuthToken != nil:
				models = provider.GrokModels
			default:
				models = provider.Models
			}

			for _, model := range models {
				if term != "" {
					matched := false
					for _, s := range []string{provider.ID, provider.Name, model.ID, model.Name} {
						if strings.Contains(strings.ToLower(s), term) {
							matched = true
							break
						}
					}
					if !matched {
						continue
					}
				}
				entry.models = append(entry.models, model.ID)
			}
			if len(entry.models) > 0 {
				slices.Sort(entry.models)
				entries[providerID] = entry
			}
		}

		// Add known but unconfigured providers from catwalk.
		for _, kp := range cfg.KnownProviders() {
			providerID := string(kp.ID)
			if _, exists := entries[providerID]; exists {
				continue
			}
			entry := &providerEntry{
				name:       kp.Name,
				configured: false,
			}
			for _, model := range kp.Models {
				if term != "" {
					matched := false
					for _, s := range []string{providerID, kp.Name, model.ID, model.Name} {
						if strings.Contains(strings.ToLower(s), term) {
							matched = true
							break
						}
					}
					if !matched {
						continue
					}
				}
				entry.models = append(entry.models, model.ID)
			}
			if len(entry.models) > 0 {
				slices.Sort(entry.models)
				entries[providerID] = entry
			}
		}

		var providerIDs []string
		for id := range entries {
			providerIDs = append(providerIDs, id)
		}
		sort.Strings(providerIDs)

		if len(providerIDs) == 0 && len(args) == 0 {
			return fmt.Errorf("no providers found")
		}
		if len(providerIDs) == 0 {
			return fmt.Errorf("no providers found matching %q", term)
		}

		if !isatty.IsTerminal(os.Stdout.Fd()) {
			for _, providerID := range providerIDs {
				entry := entries[providerID]
				for _, modelID := range entry.models {
					fmt.Println(providerID + "/" + modelID)
				}
			}
			return nil
		}

		t := tree.New()
		for _, providerID := range providerIDs {
			entry := entries[providerID]
			label := providerID
			if !entry.configured {
				label += " (not configured)"
			}
			providerNode := tree.Root(label)
			for _, modelID := range entry.models {
				providerNode.Child(modelID)
			}
			t.Child(providerNode)
		}

		cmd.Println(t)
		return nil
	},
}

// modelInfo is one usable model in `crush models --json`.
type modelInfo struct {
	Provider               string   `json:"provider"`
	ProviderName           string   `json:"provider_name"`
	Model                  string   `json:"model"`
	Name                   string   `json:"name"`
	ReasoningLevels        []string `json:"reasoning_levels"`
	DefaultReasoningEffort string   `json:"default_reasoning_effort,omitempty"`
	Fast                   bool     `json:"fast"`
}

// printModelsJSON lists the models of configured providers, which are the
// ones `crush run --model` and `crush spawn` can use, with the reasoning
// levels and fast mode each accepts.
func printModelsJSON(w io.Writer, cfg *config.Config, term string) error {
	list := []modelInfo{}
	for providerID, provider := range cfg.Providers.Seq2() {
		if provider.Disable {
			continue
		}
		models := provider.Models
		if providerID == string(catwalk.InferenceProviderOpenAI) && provider.OAuthToken != nil {
			models = provider.ChatGPTModels
		}
		for _, model := range models {
			if term != "" && !slices.ContainsFunc([]string{provider.ID, provider.Name, model.ID, model.Name}, func(s string) bool {
				return strings.Contains(strings.ToLower(s), term)
			}) {
				continue
			}
			list = append(list, modelInfo{
				Provider: providerID, ProviderName: provider.Name, Model: model.ID, Name: model.Name,
				ReasoningLevels: append([]string{}, model.ReasoningLevels...), DefaultReasoningEffort: model.DefaultReasoningEffort,
				Fast: cfg.ValidateFastMode(providerID, model.ID) == nil,
			})
		}
	}
	slices.SortFunc(list, func(a, b modelInfo) int {
		return strings.Compare(a.Provider+"/"+a.Model, b.Provider+"/"+b.Model)
	})
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(list)
}

func init() {
	modelsCmd.Flags().Bool("json", false, "Print usable models with their reasoning levels and fast mode as JSON")
	modelsCmd.Flags().Bool("refresh", false, "Refresh installed CLI model capabilities before listing models")
	rootCmd.AddCommand(modelsCmd)
}
