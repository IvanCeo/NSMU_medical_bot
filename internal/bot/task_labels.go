package bot

import (
	"fmt"
	"strings"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func taskLabels(buttons []domain.OutgoingButton) []string {
	labels := make([]string, len(buttons))
	counts := make(map[string]int, len(buttons))
	for i, b := range buttons {
		labels[i] = shortenTitle(strings.Join(strings.Fields(b.Label), " "), 60)
		counts[labels[i]]++
	}
	for i, b := range buttons {
		if counts[labels[i]] > 1 {
			suffix := fmt.Sprintf(" · #%d", b.AssignmentID)
			labels[i] = shortenTitle(labels[i], 60-len([]rune(suffix))) + suffix
		}
	}
	return labels
}

func shortenTitle(title string, limit int) string {
	runes := []rune(title)
	if len(runes) <= limit {
		return title
	}
	return string(runes[:limit-1]) + "…"
}
