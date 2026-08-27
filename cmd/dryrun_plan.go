package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

type dryRunStep struct {
	Method string         `json:"method"`
	URL    string         `json:"url"`
	Desc   string         `json:"desc,omitempty"`
	Params map[string]any `json:"params,omitempty"`
	Body   map[string]any `json:"body,omitempty"`
}

func printDryRunPlan(cmd *cobra.Command, desc string, extra map[string]any, steps []dryRunStep) error {
	plan := map[string]any{
		"dry_run": true,
		"desc":    desc,
		"api":     steps,
	}
	for k, v := range extra {
		plan[k] = v
	}
	if output, _ := cmd.Flags().GetString("output"); output == "" || output == "json" {
		return printJSON(plan)
	}
	fmt.Printf("%s\n", desc)
	for i, step := range steps {
		fmt.Printf("[%d] %s %s\n", i+1, step.Method, step.URL)
		if step.Desc != "" {
			fmt.Printf("    %s\n", step.Desc)
		}
	}
	return nil
}
