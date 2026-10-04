package workspace

import "trove.local/phase0/contracts"

// AuthorizeProviderAuth authenticates the desktop owner's live grant before an
// out-of-workspace account operation. External tools and AI actors cannot use it.
func (k *Kernel) AuthorizeProviderAuth(e contracts.ServiceExecutionContext) error {
	return requireOwnerScope(k, e)
}
