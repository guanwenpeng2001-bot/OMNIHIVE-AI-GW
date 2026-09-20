package repository

import (
	"context"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/predicate"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// canonicalPlatformSQL only accepts source-code SQL expressions from this package.
func canonicalPlatformSQL(expression string) string {
	return "CASE (" + expression + ") WHEN 'bedrock' THEN 'anthropic' WHEN 'vertex' THEN 'gemini' ELSE (" + expression + ") END"
}

// accountPlatformPredicate uses OR on the same row, never UNION or a join that
// could duplicate an account in a pool or count it twice in list statistics.
func accountPlatformPredicate(platform string) predicate.Account {
	switch platform {
	case domain.PlatformAnthropic:
		return account.Or(account.PlatformEQ(platform), account.And(account.PlatformEQ(domain.PlatformBedrock), account.TypeEQ(domain.AccountTypeBedrock)))
	case domain.PlatformGemini:
		return account.Or(account.PlatformEQ(platform), account.And(account.PlatformEQ("vertex"), account.TypeEQ(domain.AccountTypeServiceAccount)))
	case domain.PlatformBedrock:
		return account.And(account.PlatformIn(domain.PlatformBedrock, domain.PlatformAnthropic), account.TypeEQ(domain.AccountTypeBedrock))
	case "vertex":
		return account.And(account.PlatformIn("vertex", domain.PlatformGemini), account.TypeEQ(domain.AccountTypeServiceAccount))
	default:
		return account.PlatformEQ(platform)
	}
}

func accountPlatformsPredicate(platforms ...string) predicate.Account {
	predicates := make([]predicate.Account, 0, len(platforms))
	for _, platform := range platforms {
		predicates = append(predicates, accountPlatformPredicate(platform))
	}
	return account.Or(predicates...)
}

func groupPlatformPredicate(platform string) predicate.Group {
	switch platform {
	case domain.PlatformAnthropic:
		return group.PlatformIn(platform, domain.PlatformBedrock)
	case domain.PlatformGemini:
		return group.PlatformIn(platform, "vertex")
	case domain.PlatformBedrock, "vertex":
		return group.Or(group.PlatformEQ(platform), group.And(group.PlatformEQ(domain.CanonicalPlatform(platform)), group.AccountTypeEQ(domain.HostedAccountType(platform))))
	default:
		return group.PlatformEQ(platform)
	}
}

// Check hosting-only bindings while group rows are locked and before deleting
// existing memberships. Unrestricted mixed groups retain their prior semantics.
func validateHostedGroupBindings(ctx context.Context, client *dbent.Client, accountIDs, groupIDs []int64) error {
	if len(groupIDs) == 0 || len(accountIDs) == 0 {
		return nil
	}
	groups, err := client.Group.Query().Where(group.IDIn(groupIDs...), group.Or(group.AccountTypeNEQ(""), group.PlatformIn(domain.PlatformBedrock, "vertex"))).All(ctx)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return nil
	}
	accountRows, err := client.Account.Query().Where(account.IDIn(accountIDs...)).Select(account.FieldID, account.FieldPlatform, account.FieldType).All(ctx)
	if err != nil {
		return err
	}
	for _, accountRow := range accountRows {
		candidate := &service.Account{ID: accountRow.ID, Platform: accountRow.Platform, Type: accountRow.Type}
		for _, groupRow := range groups {
			restricted := &service.Group{Platform: groupRow.Platform, AccountType: groupRow.AccountType}
			if !restricted.AllowsAccount(candidate) {
				return fmt.Errorf("account type does not satisfy hosted group %d", groupRow.ID)
			}
		}
	}
	return nil
}
