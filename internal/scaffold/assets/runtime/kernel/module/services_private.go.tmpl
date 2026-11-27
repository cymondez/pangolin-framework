// Owner-private interface injection: the alias table, its Seal-driven
// resolution and the guarded param resolver live here. See services.go for the
// composition engine and the public contract pipeline this extends.
package module

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"github.com/pangolin-framework/pangolin/kernel/di"
)

// privateAliasMarker separates the engine-managed alias identity from
// user-visible token names. The NUL bytes cannot appear in Go type names, so
// a real provider can never collide with an alias identity and the marker
// check is a complete fence for hand-crafted tokens.
const privateAliasMarker = "\x00private\x00"

// privateAliasKey identifies one owner-private interface alias by its
// consuming module and the exact canonical (name, type) identity of the
// interface parameter. The same Go interface reused privately by two modules
// produces two independent aliases.
type privateAliasKey struct {
	owner string
	param tokenKey
}

// privateAlias is the resolved owner-local implementation of one private
// interface parameter of module key.owner.
type privateAlias struct {
	key        privateAliasKey
	candidates []*entry
	selected   *entry
}

// aliasName is the container identity of the alias provider: deterministic,
// collision-proof against user provider names (marker fence) and scoped to
// the consuming module.
func aliasName(key privateAliasKey) string {
	return key.param.name + privateAliasMarker + key.owner
}

// aliasToken materializes the container token of the alias provider.
func aliasToken(key privateAliasKey) di.Token {
	return tokenAdaptor{name: aliasName(key), typ: key.param.typ}
}

// sortedPrivateBindingKeys orders private binding keys deterministically.
func sortedPrivateBindingKeys(m map[privateAliasKey]*binding) []privateAliasKey {
	keys := make([]privateAliasKey, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].owner != keys[j].owner {
			return keys[i].owner < keys[j].owner
		}
		return lessTokenKey(keys[i].param, keys[j].param)
	})
	return keys
}

// sortedPrivateAliasKeys orders resolved aliases deterministically so every
// derived structure (laundering graph, provider list) is reproducible.
func sortedPrivateAliasKeys(m map[privateAliasKey]*privateAlias) []privateAliasKey {
	keys := make([]privateAliasKey, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].owner != keys[j].owner {
			return keys[i].owner < keys[j].owner
		}
		return lessTokenKey(keys[i].param, keys[j].param)
	})
	return keys
}

// privateCandidateOf reports whether entry e is a legitimate owner-private
// inference candidate for the interface parameter param. Only
// constructor-produced registrations qualify: low-level named providers
// resolve solely by their exact token identity and never become implicit
// candidates. An As contribution stays eligible for every interface its
// concrete satisfies — the public contract pipeline handles the dedicated
// contract key, and a contribution whose contract was never claimed stays
// privately accessible to its registering module — while a ReplaceContract
// exclusion suppresses the entry for the excluded contract identity only.
func privateCandidateOf(e *entry, param tokenKey) bool {
	if !e.isCtor {
		return false
	}
	return !e.excluded[param]
}

// resolvePrivateAliasesLocked infers one alias per (consuming module, private
// interface parameter) from the module's own constructor-produced concretes.
// It runs at Seal, before any factory: missing candidates, genuine ambiguity
// and dangling private bindings fail here. Canonical interface parameters
// claimed by a loaded public contract, an export or the module's own exact
// registration keep their existing paths and never gain a private alias; a
// FOREIGN unexported registration neither satisfies this module's parameters
// nor blocks its own private inference.
func (c *composition) resolvePrivateAliasesLocked() (map[privateAliasKey]*privateAlias, error) {
	aliases := map[privateAliasKey]*privateAlias{}
	constructorsByOwner := map[string][]*entry{}
	for _, e := range c.sortedEntriesLocked() {
		if e.isCtor {
			constructorsByOwner[e.owner] = append(constructorsByOwner[e.owner], e)
		}
	}
	for _, e := range c.sortedEntriesLocked() {
		if !e.isCtor {
			continue // low-level providers declare exact named identities
		}
		for _, pk := range e.declaredKeys {
			if pk.typ.Kind() != reflect.Interface {
				continue // concrete parameters resolve by exact identity
			}
			aliasKey := privateAliasKey{owner: e.owner, param: pk}
			if _, already := aliases[aliasKey]; already {
				continue
			}
			// Public authority: a loaded public contract or export wins for
			// every resolution of this key and cannot be shadowed by a local
			// match. The module's own exact registration keeps its identity
			// path; a foreign unexported one is invisible here and must not
			// shadow the owner-local inference below.
			if _, claimed := c.contracts[pk]; claimed {
				continue
			}
			if _, claimed := c.exported[pk]; claimed {
				continue
			}
			if ex, exact := c.entries[pk]; exact && ex.owner == e.owner {
				continue
			}
			alias := &privateAlias{key: aliasKey}
			for _, cand := range constructorsByOwner[e.owner] {
				if privateCandidateOf(cand, pk) && implementsOrExact(cand.typ, pk.typ) {
					alias.candidates = append(alias.candidates, cand)
				}
			}
			if len(alias.candidates) == 0 {
				return nil, c.privateAliasMissingError(e, pk)
			}
			if err := c.selectPrivateAliasLocked(alias, true); err != nil {
				return nil, err
			}
			aliases[aliasKey] = alias
		}
	}
	// Private Bind calls are explicit known private requests: after the
	// contract-resolution stage any claimed key was already promoted to the
	// public rule, so every remaining key is unclaimed and a binding for it
	// establishes the consuming module's alias even when no constructor
	// declares the parameter — Start/Stop hooks and explicit OpenScope can
	// then resolve the interface without forcing a public Export. Keys with
	// an exact own registration follow that identity instead and are reported
	// as redundant bindings.
	for _, aliasKey := range sortedPrivateBindingKeys(c.privateBindings) {
		if _, already := aliases[aliasKey]; already {
			continue
		}
		if ex, exact := c.entries[aliasKey.param]; exact && ex.owner == aliasKey.owner {
			continue // exact own registration wins; dangling check reports below
		}
		alias := &privateAlias{key: aliasKey}
		for _, cand := range constructorsByOwner[aliasKey.owner] {
			if privateCandidateOf(cand, aliasKey.param) && implementsOrExact(cand.typ, aliasKey.param.typ) {
				alias.candidates = append(alias.candidates, cand)
			}
		}
		if len(alias.candidates) == 0 {
			continue // no own implementation; dangling check reports below
		}
		if err := c.selectPrivateAliasLocked(alias, false); err != nil {
			return nil, err
		}
		aliases[aliasKey] = alias
	}
	if err := c.checkDanglingPrivateBindingsLocked(aliases); err != nil {
		return nil, err
	}
	return aliases, nil
}

// selectPrivateAliasLocked picks the unique candidate or validates the
// module's own private Bind against the candidate list. parameterDriven marks
// an alias requested by a constructor parameter: the unique-candidate
// auto-selection fully determines the implementation, making an accompanying
// Bind redundant. A bind-driven alias is established by that recorded private
// request, so its unique candidate is selected by the bind rather than
// reported as redundant.
func (c *composition) selectPrivateAliasLocked(alias *privateAlias, parameterDriven bool) error {
	pk := alias.key.param
	b := c.privateBindings[alias.key]
	switch len(alias.candidates) {
	case 1:
		if b != nil && parameterDriven {
			return fmt.Errorf("redundant private binding for interface %s owned by module %s: candidate %s from module %s is the only implementation",
				pk.name, alias.key.owner, alias.candidates[0].typ, alias.candidates[0].owner)
		}
		alias.selected = alias.candidates[0]
		return nil
	default:
		if b == nil {
			return privateAmbiguityError(pk, alias.key.owner, alias.candidates)
		}
		for _, cand := range alias.candidates {
			if cand.typ == b.typ {
				alias.selected = cand
				return nil
			}
		}
		return fmt.Errorf("module %s: private binding for interface %s does not match any candidate (%s)",
			alias.key.owner, pk.name, candidateListLocked(alias.candidates))
	}
}

// privateAmbiguityError keeps the ambiguity diagnostics family and points at
// the private Bind: exporting the contract is not a fix for multiple private
// candidates.
func privateAmbiguityError(pk tokenKey, owner string, candidates []*entry) error {
	return fmt.Errorf("ambiguous private interface %s owned by module %s: %s; bind one of the module's own candidates with Services.Bind(module.Contract[<interface>](), <implementation>), or keep exactly one own implementation; Export is not a fix for multiple private candidates",
		pk.name, owner, candidateListLocked(candidates))
}

// checkDanglingPrivateBindingsLocked rejects private bindings that cannot
// apply after the graph is loaded: after bind-driven alias inference, a
// still-unclaimed binding fails only when the module has no own
// implementation to select among — the guidance is to register an own
// constructor, or to move the interface deliberately onto the public
// contract route with a direct dependency, not to force an Export.
func (c *composition) checkDanglingPrivateBindingsLocked(aliases map[privateAliasKey]*privateAlias) error {
	for _, aliasKey := range sortedPrivateBindingKeys(c.privateBindings) {
		if _, used := aliases[aliasKey]; used {
			continue
		}
		b := c.privateBindings[aliasKey]
		key := aliasKey.param
		if e, exact := c.entries[key]; exact {
			if e.owner == aliasKey.owner {
				return fmt.Errorf("module %s: private binding for interface %s is redundant: module %s already registers the exact implementation",
					aliasKey.owner, key.name, aliasKey.owner)
			}
			return fmt.Errorf("module %s: private binding for interface %s cannot apply: an exact implementation of %s is registered by module %s and is not visible to this module",
				aliasKey.owner, key.name, key.name, e.owner)
		}
		return fmt.Errorf("module %s binds interface %s: no module exported this contract and no constructor registered by module %s produces a compatible implementation; register an own implementation constructor for the private binding to select, or deliberately export the contract and declare a direct dependency on its owner for cross-module use",
			b.owner, key.name, aliasKey.owner)
	}
	return nil
}

// privateAliasMissingError names the consuming module and interface, keeps
// the legacy "missing provider" diagnostics family, and points private users
// at registering an own constructor or at the deliberate public route; a
// foreign exact registration is mentioned because it would otherwise be
// mistaken for a satisfiable path.
func (c *composition) privateAliasMissingError(consumer *entry, pk tokenKey) error {
	message := fmt.Sprintf("module %s (constructor %s): missing provider %s: no constructor registered by this module produces a compatible implementation; private inference only considers constructors registered by the same module (never named providers or implementations of other modules)",
		consumer.owner, consumer.key.name, pk.name)
	if e, exact := c.entries[pk]; exact && e.owner != consumer.owner {
		message += fmt.Sprintf("; an exact implementation registered by module %s is not visible to this module", e.owner)
	}
	message += "; register an own implementation constructor for private use, or deliberately export the interface contract and declare a direct dependency on its owner for cross-module use"
	return fmt.Errorf("%s", message)
}

// ownerPrivateDynamicLocked backs the restricted resolvers for owner-private
// unclaimed canonical interface tokens that the alias table does not cover:
// a unique own implementation selects its existing immutable provider at
// Resolve (no registry mutation, no unseal, no global resolver), genuine
// ambiguity reports the private Bind guidance, and keys with no own
// candidate, any claim or an own exact registration stay on the ordinary
// visibility path. Foreign registrations are never returned.
func (c *composition) ownerPrivateDynamicLocked(owner string, id tokenKey) (di.Token, bool, error) {
	if id.typ == nil || id.typ.Kind() != reflect.Interface || id.name != canonicalTokenName(id.typ) {
		return nil, false, nil
	}
	if _, claimed := c.contracts[id]; claimed {
		return nil, false, nil
	}
	if _, claimed := c.exported[id]; claimed {
		return nil, false, nil
	}
	if e, exact := c.entries[id]; exact && e.owner == owner {
		return nil, false, nil // own exact registration resolves directly
	}
	var candidates []*entry
	for _, cand := range c.sortedEntriesLocked() {
		if cand.owner != owner {
			continue
		}
		if privateCandidateOf(cand, id) && implementsOrExact(cand.typ, id.typ) {
			candidates = append(candidates, cand)
		}
	}
	switch len(candidates) {
	case 1:
		return candidates[0].token, true, nil
	default:
		if len(candidates) > 1 {
			return nil, true, privateAmbiguityError(id, owner, candidates)
		}
		return nil, false, nil
	}
}

// restrictedResolveTokenLocked returns the container token owner's restricted
// resolver must use for key: the engine alias token when the key resolves
// through the owner-private alias table, the dynamically selected own
// provider token for a unique lifecycle-only private interface, or the key
// itself for the ordinary visibility path.
func (c *composition) restrictedResolveTokenLocked(owner string, key di.Token, id tokenKey) (di.Token, bool, error) {
	if alias, ok := c.privateAliases[privateAliasKey{owner: owner, param: id}]; ok {
		return aliasToken(alias.key), true, nil
	}
	if tok, handled, err := c.ownerPrivateDynamicLocked(owner, id); err != nil || handled {
		return tok, handled, err
	}
	return key, false, nil
}

// privateParamResolver translates constructor parameter tokens into the
// caller module's owner-private alias tokens while forwarding every other
// resolution untouched to the guarded factory resolver. The substitution
// table is built from the consumer's declared keys only, so any resolver
// access that is not declared stays raw and is rejected by the di factory
// guard; the resolver exists only for the duration of one factory invocation
// and never resolves through a global unguarded resolver.
type privateParamResolver struct {
	di.Resolver
	table map[tokenKey]di.Token
}

func (r *privateParamResolver) Resolve(ctx context.Context, key di.Token) (any, error) {
	if k, err := keyOf(key); err == nil {
		if alias, ok := r.table[k]; ok {
			key = alias
		}
	}
	return r.Resolver.Resolve(ctx, key)
}

// privateParamFactory wraps a constructor factory so its interface parameters
// resolve through the module's private alias providers without touching any
// public resolution path.
func privateParamFactory(inner di.Factory, table map[tokenKey]di.Token) di.Factory {
	return func(ctx context.Context, resolver di.Resolver) (any, di.Cleanup, error) {
		return inner(ctx, &privateParamResolver{Resolver: resolver, table: table})
	}
}

// privateAliasProvider builds the container-side alias provider: a passthrough
// delegation with the selected implementation's lifetime, mirroring the public
// contract delegation providers. Its guarded dependency edge is the selected
// implementation only.
func privateAliasProvider(alias *privateAlias) di.Provider {
	token := aliasToken(alias.key)
	sel := alias.selected
	return di.Provider{
		Key:       token,
		Lifetime:  sel.lifetime,
		DependsOn: []di.Token{sel.token},
		Factory: func(ctx context.Context, resolver di.Resolver) (any, di.Cleanup, error) {
			v, err := resolver.Resolve(ctx, sel.token)
			if err != nil {
				return nil, nil, err
			}
			return v, nil, nil
		},
	}
}
