package cli

import (
	"fmt"
	"sort"
	"strings"

	cmds "github.com/fgeth/fg-ipfs-cmds"
	levenshtein "github.com/texttheater/golang-levenshtein/levenshtein"
)

// Make a custom slice that can be sorted by its levenshtein value
type suggestionSlice []*suggestion

type suggestion struct {
	cmd         string
	levenshtein int
}

func (s suggestionSlice) Len() int {
	return len(s)
}

func (s suggestionSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s suggestionSlice) Less(i, j int) bool {
	return s[i].levenshtein < s[j].levenshtein
}

func suggestUnknownCmd(args []string, root *cmds.Command) []string {
	if root == nil {
		return nil
	}

	arg := args[0]
	var suggestions []string
	sortableSuggestions := make(suggestionSlice, 0)
	var sFinal []string
	const MinLevenshtein = 3

	var options levenshtein.Options = levenshtein.Options{
		InsCost: 1,
		DelCost: 3,
		SubCost: 2,
		Matches: func(sourceCharacter rune, targetCharacter rune) bool {
			return sourceCharacter == targetCharacter
		},
	}

	// Start with a simple strings.Contains check
	for name := range root.Subcommands {
		if strings.Contains(arg, name) {
			suggestions = append(suggestions, name)
		}
	}

	// If the string compare returns a match, return
	if len(suggestions) > 0 {
		return suggestions
	}

	for name := range root.Subcommands {
		lev := levenshtein.DistanceForStrings([]rune(arg), []rune(name), options)
		if lev <= MinLevenshtein {
			sortableSuggestions = append(sortableSuggestions, &suggestion{name, lev})
		}
	}
	sort.Sort(sortableSuggestions)

	for _, j := range sortableSuggestions {
		sFinal = append(sFinal, j.cmd)
	}
	return sFinal
}

func printSuggestions(inputs []string, root *cmds.Command) (err error) {

	suggestions := suggestUnknownCmd(inputs, root)

	if len(suggestions) > 1 {
		//lint:ignore ST1005 user facing error
		err = fmt.Errorf("Unknown Command \"%s\"\n\nDid you mean any of these?\n\n\t%s", inputs[0], strings.Join(suggestions, "\n\t"))

	} else if len(suggestions) > 0 {
		//lint:ignore ST1005 user facing error
		err = fmt.Errorf("Unknown Command \"%s\"\n\nDid you mean this?\n\n\t%s", inputs[0], suggestions[0])

	} else {
		//lint:ignore ST1005 user facing error
		err = fmt.Errorf("Unknown Command \"%s\"\n", inputs[0])
	}
	return
}
