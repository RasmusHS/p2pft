package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// promptYN writes a prompt to stderr and reads a single line from stdin.
// Returns true only if the user typed "y" or "yes" (case-insensitive).
// Empty input or anything else returns false — defaulting to NO is the
// safer choice for an inbound file.
func promptYN(question string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N]: ", question)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		// Non-TTY (piped, redirected) or EOF — treat as no.
		return false
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}
