package typescript

import (
	"path/filepath"

	"github.com/mikn/rules_typescript/ts/tools/explainfiles"
)

type resolutionCandidate struct {
	from, specifier, path string
}

func relativeResolutionCandidates(root string, observations []explainfiles.ResolutionCandidate) ([]resolutionCandidate, error) {
	var candidates []resolutionCandidate
	for _, observation := range observations {
		if observation.Kind != explainfiles.Import {
			continue
		}
		from, err := filepath.Rel(root, observation.From)
		if err != nil {
			return nil, err
		}
		candidate, err := filepath.Rel(root, observation.Path)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, resolutionCandidate{
			from: filepath.ToSlash(from), specifier: observation.Specifier, path: filepath.ToSlash(candidate),
		})
	}
	return candidates, nil
}
