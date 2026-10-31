package bot

import (
	"strings"
	"testing"

	"github.com/IvanCeo/NSMU_medical_bot/internal/domain"
)

func TestTaskLabels_TopicsUnicodeAndCollisions(t *testing.T) {
	long := strings.Repeat("Тема🩺", 20)
	buttons := []domain.OutgoingButton{{Label: "  Анатомия\n костей\t", AssignmentID: 1}, {Label: long + "А", AssignmentID: 2}, {Label: long + "Б", AssignmentID: 3}}
	labels := taskLabels(buttons)
	if labels[0] != "Анатомия костей" || labels[1] == labels[2] || !strings.HasSuffix(labels[1], " · #2") || !strings.HasSuffix(labels[2], " · #3") {
		t.Fatalf("labels=%v", labels)
	}
	for _, label := range labels {
		if len([]rune(label)) > 60 {
			t.Fatalf("long label: %s", label)
		}
	}
}
