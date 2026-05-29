package signaling

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// wordlist is a placeholder. For production use, swap in a larger curated list
// (e.g. EFF short word list, ~1296 words) to increase entropy per word.
// Current entropy: log2(len(wordlist)) * 3 bits.
var wordlist = []string{
	"amber", "anchor", "apple", "arrow",
	"basket", "bridge", "candle", "cliff",
	"copper", "dahlia", "ember", "falcon",
	"forest", "garnet", "harbor", "indigo",
	"jasper", "kettle", "lantern", "meadow",
	"nectar", "orchard", "pebble", "quartz",
	"raven", "saffron", "tundra", "velvet",
	"willow", "yarrow", "zephyr", "obsidian",
}

// GenerateCode returns a human-friendly transfer code like "amber-forest-quartz".
func GenerateCode() (string, error) {
	words := make([]string, 3)
	max := big.NewInt(int64(len(wordlist)))
	for i := range words {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("random: %w", err)
		}
		words[i] = wordlist[n.Int64()]
	}
	return fmt.Sprintf("%s-%s-%s", words[0], words[1], words[2]), nil
}
