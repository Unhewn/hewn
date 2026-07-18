package main

import (
	"fmt"
	"os"

	"github.com/Unhewn/hewn/internal/tui"

	"github.com/spf13/cobra"
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "hewn",
		Short: "hewn - A minimalist Go agent harness",
		Run: func(cmd *cobra.Command, args []string) {
			// Start TUI by default
			tui.Start()
		},
	}

	// Headless mode example
	rootCmd.Flags().StringP("prompt", "p", "", "Run prompt and exit (headless)")
	rootCmd.Flags().String("model", "gpt-4o", "Model to use")

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
