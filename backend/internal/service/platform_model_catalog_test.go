package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestPlatformModelCatalogDoesNotBorrowClaudeDefaults(t *testing.T) {
	for _, descriptor := range domain.PlatformDescriptors() {
		t.Run(descriptor.ID, func(t *testing.T) {
			ids := DefaultModelIDsForPlatform(descriptor.ID)
			if descriptor.CNProvider && descriptor.ID != PlatformQianwen {
				require.Empty(t, ids)
			}
			if len(ids) > 0 {
				original := ids[0]
				ids[0] = "mutated"
				require.Equal(t, original, DefaultModelIDsForPlatform(descriptor.ID)[0])
			}
		})
	}
	require.Empty(t, DefaultModelIDsForPlatform("unknown"))
	require.Contains(t, DefaultModelIDsForPlatform(PlatformComposite), DefaultModelIDsForPlatform(PlatformCursor)[0])
}
