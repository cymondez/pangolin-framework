package module

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pangolin-framework/pangolin/kernel/di"
)

// RootBuilder is the hook-facing registration view the root runtime hands to
// each module for the duration of one hook. The runtime owns window
// enforcement: after a hook returns it revokes the module's registrar, so any
// registrar stashed by the module is rejected by the engine on later use.
type RootBuilder interface {
	SetDefault(key, value string) error
	Services() Services
	Provide(p di.Provider) error
	Replace(p di.Provider, reason string) error
}

// Optional root-module hooks; a module implements only what it needs and the
// runtime treats missing hooks as no-ops.
type PreConfigurer interface{ PreConfigure(RootBuilder) error }

type Configurer interface{ Configure(RootBuilder) error }

type PostConfigurer interface{ PostConfigure(RootBuilder) error }

type Starter interface {
	Start(context.Context, Runtime) error
}

type Stopper interface {
	Stop(context.Context, Runtime) error
}

// Services registers constructors and contract declarations for one trusted
// module. Every operation is attributed to the owner the stamping host
// declared when handing out the registrar.
type Services interface {
	// Add registers a constructor; the key is the exact returned type.
	// Lifetime defaults to Transient.
	Add(constructor any, opts ...AddOption) error
	// Replace re-registers a module-owned constructor producing the same
	// exact type; a non-empty reason is recorded.
	Replace(constructor any, reason string, opts ...AddOption) error
	// Export establishes this module as the single owner of a contract.
	Export(contract di.Token) error
	// Bind selects one implementation for a contract that has more than one
	// candidate; only the contract owner may bind.
	Bind(contract di.Token, impl any) error
	// ReplaceBinding swaps an existing binding owned by this module.
	ReplaceBinding(contract di.Token, impl any, reason string) error
	// ReplaceContract swaps this module's own explicit As contribution to a
	// contract it owns with a new constructor, preserving contract identity.
	ReplaceContract(contract di.Token, constructor any, reason string, opts ...AddOption) error
}

// Composition is the trusted per-module constructor engine over a root plan.
// It owns its di registry; Seal returns the canonical di container after all
// contract, visibility and lifetime validation has run without executing any
// constructor.
type Composition interface {
	// Services returns a registrar stamped with the owner. Revoke makes
	// previously handed handles unusable; a later call hands a fresh one.
	Services(owner string) (Services, error)
	// Provide registers a low-level provider with its exact token identity.
	Provide(owner string, p di.Provider) error
	// Replace swaps an existing low-level provider owned by this module,
	// preserving provider-owned token identity; reason is required.
	Replace(owner string, p di.Provider, reason string) error
	// Revoke invalidates previously handed Services handles for the owner.
	Revoke(owner string)
	// Seal validates the whole graph and returns the canonical di container.
	// Seal is one-shot, deterministic and runs no constructor eagerly except
	// the di eager singleton construction of the sealed container.
	Seal(ctx context.Context) (di.Container, error)
	// LifecycleResolver returns the post-seal restricted resolver of a
	// module: its own registrations plus direct dependencies' exported
	// tokens. Each Resolve runs inside one operation scope.
	LifecycleResolver(owner string) (di.Resolver, error)
	// ReplacementLog returns the provenance record of replacement
	// operations: constructor Replace, low-level Replace, binding and
	// contract implementation swaps, in registration order, each with its
	// recorded source module and reason.
	ReplacementLog() []string
}

// NewComposition validates the root plan and builds the engine. The plan is
// the loader output (Plan.Ordered topological order keyed by name); the
// engine reads only names and direct dependency names from it.
func NewComposition(plan RootPlan, cleanupTimeout time.Duration) (Composition, error) {
	if cleanupTimeout <= 0 {
		return nil, fmt.Errorf("composition cleanup timeout must be positive")
	}
	if len(plan.Plan.Ordered) == 0 {
		return nil, fmt.Errorf("composition requires at least one module")
	}
	modules := map[string]*modulePlan{}
	for _, m := range plan.Plan.Ordered {
		if _, dup := modules[m.Name]; dup {
			return nil, fmt.Errorf("composition duplicate module %s", m.Name)
		}
		modules[m.Name] = &modulePlan{name: m.Name, direct: map[string]bool{}}
	}
	for _, m := range plan.Plan.Ordered {
		mod := modules[m.Name]
		seen := map[string]bool{}
		for _, dep := range m.DependsOn {
			if dep.Name == m.Name {
				return nil, fmt.Errorf("module %s depends on itself", m.Name)
			}
			if _, ok := modules[dep.Name]; !ok {
				return nil, fmt.Errorf("module %s: dependency %s is not part of the composition", m.Name, dep.Name)
			}
			if seen[dep.Name] {
				return nil, fmt.Errorf("module %s: duplicate dependency %s", m.Name, dep.Name)
			}
			seen[dep.Name] = true
			mod.direct[dep.Name] = true
		}
	}
	return &composition{
		modules:         modules,
		epoch:           map[string]int{},
		sessions:        map[string]*ownerSession{},
		entries:         map[tokenKey]*entry{},
		contracts:       map[tokenKey]*contractDef{},
		bindings:        map[tokenKey]*binding{},
		privateBindings: map[privateAliasKey]*binding{},
		exported:        map[tokenKey]string{},
		cleanupTimeout:  cleanupTimeout,
	}, nil
}

// AddOption mutates constructor registration options.
type AddOption func(*addOptions)

type addOptions struct {
	lifetime       di.Lifetime
	lifetimeSet    bool
	contributed    di.Token
	contributedSet bool
	conflict       bool
}

// Singleton opts the constructor into shared singleton lifetime.
func Singleton() AddOption { return lifetimeOption(di.Singleton) }

// Transient opts the constructor into transient lifetime (the default, kept
// explicit for readable registration sites).
func Transient() AddOption { return lifetimeOption(di.Transient) }

// Operation opts the constructor into per-operation lifetime; such services
// resolve only through scoped resolvers and must never be captured by a
// singleton, directly or through any Transient edge.
func Operation() AddOption { return lifetimeOption(di.Operation) }

func lifetimeOption(l di.Lifetime) AddOption {
	return func(o *addOptions) {
		if o.lifetimeSet && o.lifetime != l {
			o.conflict = true
		}
		o.lifetime, o.lifetimeSet = l, true
	}
}

// As marks a constructor as an explicit cross-owner contribution to an
// interface contract exported by another module. The implementing module must
// directly depend on the contract owner; without As the constructor stays
// module-internal.
func As(contract di.Token) AddOption {
	return func(o *addOptions) {
		if o.contributedSet {
			o.conflict = true
		}
		o.contributed, o.contributedSet = contract, true
	}
}

// tokenKey pairs the token name with its type identity, mirroring the di
// (Name, Type) token identity.
type tokenKey struct {
	name string
	typ  reflect.Type
}

// zeroTokenKey marks "no contract association"; comparing against the zero
// value avoids a composite literal inside if statement headers.
var zeroTokenKey = tokenKey{}

func keyOf(token di.Token) (tokenKey, error) {
	if token == nil {
		return tokenKey{}, errors.New("nil DI token")
	}
	if v := reflect.ValueOf(token); v.Kind() == reflect.Pointer && v.IsNil() {
		return tokenKey{}, errors.New("nil DI token")
	}
	typ := token.Type()
	if typ == nil {
		return tokenKey{}, fmt.Errorf("invalid DI token %q", token.Name())
	}
	id := tokenKey{token.Name(), typ}
	if strings.TrimSpace(id.name) == "" {
		return id, fmt.Errorf("invalid DI token: empty name")
	}
	return id, nil
}

// canonicalTokenName derives the deterministic contract identity name from
// the type itself so every module computing the token for the same type
// agrees without a shared name string.
func canonicalTokenName(t reflect.Type) string {
	if t == nil {
		return "<nil>"
	}
	if t.Name() != "" && t.PkgPath() != "" {
		name := t.PkgPath() + "." + t.Name()
		if display := t.String(); len(display) > len(name) {
			// Generic instantiations append their type arguments; keep them
			// so distinct instantiations stay distinct.
			name += display[len(name):]
		}
		return name
	}
	if t.Kind() == reflect.Pointer {
		return "*" + canonicalTokenName(t.Elem())
	}
	return t.String()
}

// Contract returns the canonical contract token of T: a di token whose name
// is deterministically derived from the type identity.
func Contract[T any]() di.Token {
	return di.NewKey[T](canonicalTokenName(reflect.TypeFor[T]()))
}

// Service returns the canonical exact-concrete-service token of T.
func Service[T any]() di.Token {
	return di.NewKey[T](canonicalTokenName(reflect.TypeFor[T]()))
}

// tokenAdaptor materializes a canonical di token for a runtime type.
type tokenAdaptor struct {
	name string
	typ  reflect.Type
}

func (t tokenAdaptor) Name() string       { return t.name }
func (t tokenAdaptor) Type() reflect.Type { return t.typ }

func canonicalTokenFor(t reflect.Type) di.Token { return tokenAdaptor{canonicalTokenName(t), t} }

type modulePlan struct {
	name   string
	direct map[string]bool
}

type entry struct {
	key          tokenKey
	token        di.Token
	typ          reflect.Type
	owner        string
	lifetime     di.Lifetime
	declaredKeys []tokenKey
	tokenDeps    []di.Token // deduplicated, order-preserving
	factory      di.Factory
	isCtor       bool
	// contract is non-empty when this constructor is an explicit As
	// contribution to that contract.
	contract tokenKey
	// excluded records contract-scoped exclusions created by ReplaceContract:
	// the entry stays a plain service (other keys it satisfies keep working)
	// but is no longer a candidate for the excluded contract key only.
	excluded map[tokenKey]bool
}

type contractDef struct {
	token di.Token
	typ   reflect.Type
	owner string
}

type binding struct {
	owner string
	impl  any
	typ   reflect.Type
	token di.Token
}

// excludeContract scopes a replacement exclusion to one contract key so the
// concrete keeps serving every other interface it still satisfies.
func (e *entry) excludeContract(key tokenKey) {
	if e.excluded == nil {
		e.excluded = map[tokenKey]bool{}
	}
	e.excluded[key] = true
}

type composition struct {
	mu            sync.Mutex
	modules       map[string]*modulePlan
	epoch         map[string]int
	sessions      map[string]*ownerSession
	entries       map[tokenKey]*entry
	contractOrder []*contractDef
	contracts     map[tokenKey]*contractDef
	bindings      map[tokenKey]*binding
	// privateBindings records owner-private Bind calls for unclaimed
	// canonical interface keys, keyed (consuming module, param identity).
	privateBindings map[privateAliasKey]*binding
	// privateAliases is the resolved owner-private alias table, populated by
	// provisionLocked at Seal; afterwards it is read-only under the lock.
	privateAliases map[privateAliasKey]*privateAlias
	exported       map[tokenKey]string
	replacementLog []string
	sealed         bool
	container      di.Container
	cleanupTimeout time.Duration
}

// ownerSession serializes all operations of the registrar handles handed out
// for one module: the epoch check and the mutation run atomically under the
// session lock with the composition lock held afterward, so Revoke (which
// bumps the epoch and then waits on the session) can never interleave a
// post-revocation write.
type ownerSession struct {
	mu sync.Mutex
}

type ownerServices struct {
	c       *composition
	owner   string
	gen     int
	session *ownerSession
}

// acquire locks the session, verifies the epoch against the current
// composition state and keeps both locks while the mutation runs.
func (s *ownerServices) acquire() (*composition, error) {
	s.session.mu.Lock()
	c := s.c
	c.mu.Lock()
	if s.gen != c.epoch[s.owner] {
		c.mu.Unlock()
		s.session.mu.Unlock()
		return nil, fmt.Errorf("module %s: registrar handle was revoked", s.owner)
	}
	if c.sealed {
		c.mu.Unlock()
		s.session.mu.Unlock()
		return nil, fmt.Errorf("composition is sealed")
	}
	return c, nil
}

func (s *ownerServices) release() {
	s.c.mu.Unlock()
	s.session.mu.Unlock()
}

func (s *ownerServices) Add(constructor any, opts ...AddOption) error {
	c, err := s.acquire()
	if err != nil {
		return err
	}
	defer s.release()
	return c.addConstructorLocked(s.owner, constructor, opts)
}

func (s *ownerServices) Replace(constructor any, reason string, opts ...AddOption) error {
	c, err := s.acquire()
	if err != nil {
		return err
	}
	defer s.release()
	return c.replaceConstructorLocked(s.owner, constructor, reason, opts)
}

func (s *ownerServices) Export(contract di.Token) error {
	c, err := s.acquire()
	if err != nil {
		return err
	}
	defer s.release()
	return c.exportLocked(s.owner, contract)
}

func (s *ownerServices) Bind(contract di.Token, impl any) error {
	c, err := s.acquire()
	if err != nil {
		return err
	}
	defer s.release()
	return c.bindLocked(s.owner, contract, impl, "Bind", "")
}

func (s *ownerServices) ReplaceBinding(contract di.Token, impl any, reason string) error {
	c, err := s.acquire()
	if err != nil {
		return err
	}
	defer s.release()
	return c.bindLocked(s.owner, contract, impl, "ReplaceBinding", reason)
}

func (s *ownerServices) ReplaceContract(contract di.Token, constructor any, reason string, opts ...AddOption) error {
	c, err := s.acquire()
	if err != nil {
		return err
	}
	defer s.release()
	return c.replaceContractLocked(s.owner, contract, constructor, reason, opts)
}

func (c *composition) Services(owner string) (Services, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.modules[owner]; !ok {
		return nil, fmt.Errorf("composition has no module %s", owner)
	}
	if c.sealed {
		return nil, fmt.Errorf("composition is sealed")
	}
	gen := c.epoch[owner]
	session := c.sessions[owner]
	if session == nil {
		session = &ownerSession{}
		c.sessions[owner] = session
	}
	return &ownerServices{c: c, owner: owner, gen: gen, session: session}, nil
}

// Revoke invalidates every previously handed registrar handle of the module
// and waits for an in-flight mutation (if any) to finish before returning.
func (c *composition) Revoke(owner string) {
	c.mu.Lock()
	if _, ok := c.modules[owner]; !ok {
		c.mu.Unlock()
		return
	}
	c.epoch[owner]++
	session := c.sessions[owner]
	c.mu.Unlock()
	if session != nil {
		session.mu.Lock()
		session.mu.Unlock()
	}
}

func (c *composition) Provide(owner string, p di.Provider) error {
	return c.put(owner, p, "", false)
}

func (c *composition) Replace(owner string, p di.Provider, reason string) error {
	return c.put(owner, p, reason, true)
}

func (c *composition) put(owner string, p di.Provider, reason string, replace bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.modules[owner]; !ok {
		return fmt.Errorf("composition has no module %s", owner)
	}
	if c.sealed {
		return fmt.Errorf("composition is sealed")
	}
	key, err := keyOf(p.Key)
	if err != nil {
		return err
	}
	if strings.Contains(key.name, privateAliasMarker) {
		return fmt.Errorf("module %s: provider identity %s uses the engine-managed private alias namespace", owner, key.name)
	}
	if p.Factory == nil || p.Lifetime > di.Operation {
		return fmt.Errorf("module %s: invalid provider %s", owner, key.name)
	}
	deps, err := keyList(p.DependsOn)
	if err != nil {
		return fmt.Errorf("module %s: provider %s: %w", owner, key.name, err)
	}
	if replace {
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("module %s: replacement of %s requires a reason", owner, key.name)
		}
		existing, ok := c.entries[key]
		if !ok || existing.isCtor {
			return fmt.Errorf("module %s: replacement requires an existing provider %s", owner, key.name)
		}
		previousOwner := existing.owner
		if existing.owner != owner {
			// Cross-module replacement is only legal for an exported
			// provider: the replacer must directly depend on the export
			// owner (the contract owner); unexported (private) foreign
			// providers are never replaceable. The exact token identity and
			// the export/contract owner are retained; only the implementation
			// source moves.
			export, exported := c.exported[key]
			if !exported || (owner != export && !c.modules[owner].direct[export]) {
				return fmt.Errorf("module %s cannot replace %s owned by module %s: cross-module replacement requires an exported provider and a direct dependency on its export owner module %s", owner, key.name, existing.owner, export)
			}
		}
		existing.lifetime, existing.tokenDeps, existing.declaredKeys = p.Lifetime, deps.tokens, deps.keys
		existing.factory = p.Factory
		existing.owner = owner
		if previousOwner != owner {
			c.logLocked("module %s replaced provider %s (previous source module %s, export owner %s): %s", owner, key.name, previousOwner, c.exported[key], reason)
		} else {
			c.logLocked("module %s replaced provider %s: %s", owner, key.name, reason)
		}
		return nil
	}
	if existing, ok := c.entries[key]; ok {
		return fmt.Errorf("service %s is registered by module %s and module %s", key.name, existing.owner, owner)
	}
	c.entries[key] = &entry{
		key: key, token: p.Key, typ: p.Key.Type(), owner: owner,
		lifetime: p.Lifetime, declaredKeys: deps.keys, tokenDeps: deps.tokens,
		factory: p.Factory, isCtor: false,
	}
	return nil
}

func (c *composition) exportLocked(owner string, contract di.Token) error {
	key, err := keyOf(contract)
	if err != nil {
		return err
	}
	typ := contract.Type()
	if prev, dup := c.exported[key]; dup {
		return fmt.Errorf("contract %s is already exported by module %s; export ownership is established exactly once (module %s duplicate rejected)", key.name, prev, owner)
	}
	if key.name != canonicalTokenName(typ) {
		// Deliberate export bridging for a custom-named low-level provider:
		// the exact (name, type) identity is preserved; no canonical
		// renaming. Contract semantics (As/Bind/ReplaceContract) stay
		// restricted to canonical tokens.
		e, ok := c.entries[key]
		if !ok || e.owner != owner {
			return fmt.Errorf("module %s cannot export %s: custom-named exports require owning the exact provider", owner, key.name)
		}
		c.exported[key] = owner
		return nil
	}
	if typ.Kind() == reflect.Interface {
		def, ok := c.contracts[key]
		if !ok {
			def = &contractDef{token: contract, typ: typ}
			c.contracts[key] = def
			c.contractOrder = append(c.contractOrder, def)
		}
		def.owner = owner
	} else {
		e, ok := c.entries[key]
		if !ok || e.owner != owner {
			return fmt.Errorf("module %s cannot export %s: it does not own a registration with this exact identity", owner, key.name)
		}
	}
	c.exported[key] = owner
	return nil
}

// bindLocked records a binding request. The binding source is retained here;
// its legality against the contract owner and the implementation owner is
// decided at Seal, where the candidate set is known.
func (c *composition) bindLocked(owner string, contract di.Token, impl any, op, reason string) error {
	key, err := keyOf(contract)
	if err != nil {
		return err
	}
	if err := requireCanonicalToken(key, contract.Type()); err != nil {
		return fmt.Errorf("module %s: %w", owner, err)
	}
	if impl == nil || reflect.TypeOf(impl) == nil {
		return fmt.Errorf("module %s: %s requires a non-nil implementation value or typed pointer for type identification", owner, op)
	}
	if _, claimed := c.exported[key]; claimed {
		return c.bindPublicLocked(owner, key, impl, op, reason, contract)
	}
	if _, public := c.contracts[key]; public {
		return c.bindPublicLocked(owner, key, impl, op, reason, contract)
	}
	if key.typ.Kind() != reflect.Interface {
		// Concrete or otherwise non-contract targets keep the pre-existing
		// registration-accepts/seal-rejects behavior.
		return c.bindPublicLocked(owner, key, impl, op, reason, contract)
	}
	// Unclaimed canonical interface: owner-private binding territory, keyed
	// (consuming module, param identity). Validity is judged at Seal, after
	// the candidate set is known; binding before a possible later export
	// follows the public rule via promotion.
	return c.bindPrivateLocked(owner, key, impl, op, reason)
}

// bindPublicLocked keeps the pre-existing public-contract binding semantics:
// one binding per contract key, owner legality decided at Seal.
func (c *composition) bindPublicLocked(owner string, key tokenKey, impl any, op, reason string, contract di.Token) error {
	existing, has := c.bindings[key]
	if op == "ReplaceBinding" {
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("module %s: ReplaceBinding of %s requires a reason", owner, key.name)
		}
		if !has || existing.owner != owner {
			return fmt.Errorf("module %s: ReplaceBinding requires an existing binding for contract %s owned by this module", owner, key.name)
		}
		c.logLocked("module %s replaced binding for contract %s: %s", owner, key.name, reason)
	} else if has {
		return fmt.Errorf("contract %s already has a binding registered by module %s; duplicate %s rejected", key.name, existing.owner, op)
	}
	c.bindings[key] = &binding{owner: owner, impl: impl, typ: reflect.TypeOf(impl), token: contract}
	return nil
}

// bindPrivateLocked records a private ambiguous-candidate selection. It is
// visible only to the binding module's own private aliases: direct
// dependents, public visibility and the global registry never expose it.
func (c *composition) bindPrivateLocked(owner string, key tokenKey, impl any, op, reason string) error {
	aliasKey := privateAliasKey{owner: owner, param: key}
	has := c.privateBindings[aliasKey] != nil
	if op == "ReplaceBinding" {
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("module %s: ReplaceBinding of %s requires a reason", owner, key.name)
		}
		if !has {
			return fmt.Errorf("module %s: ReplaceBinding requires an existing private binding for interface %s owned by this module", owner, key.name)
		}
		c.logLocked("module %s replaced private binding for interface %s: %s", owner, key.name, reason)
	} else if has {
		return fmt.Errorf("module %s: private binding for interface %s is already registered by this module; duplicate %s rejected", owner, key.name, op)
	}
	c.privateBindings[aliasKey] = &binding{owner: owner, impl: impl, typ: reflect.TypeOf(impl), token: tokenAdaptor{name: key.name, typ: key.typ}}
	return nil
}

// promotePrivateBindingsLocked moves private bindings onto keys that were
// exported (or became public contracts) after the Bind call. Order of
// Add/Bind/Export is therefore irrelevant; the public assembly rule decides
// legality at Seal and keeps one binding per public contract key.
func (c *composition) promotePrivateBindingsLocked() error {
	for _, aliasKey := range sortedPrivateBindingKeys(c.privateBindings) {
		pb := c.privateBindings[aliasKey]
		if _, claimed := c.exported[aliasKey.param]; !claimed {
			if _, public := c.contracts[aliasKey.param]; !public {
				continue
			}
		}
		if existing, dup := c.bindings[aliasKey.param]; dup {
			return fmt.Errorf("module %s cannot bind contract %s: a conflicting binding is already registered by module %s", aliasKey.owner, aliasKey.param.name, existing.owner)
		}
		c.bindings[aliasKey.param] = pb
		delete(c.privateBindings, aliasKey)
	}
	return nil
}

func (c *composition) addConstructorLocked(owner string, constructor any, opts []AddOption) error {
	built, err := c.buildConstructor(owner, constructor, opts)
	if err != nil {
		return err
	}
	if built.opts.conflict {
		return fmt.Errorf("module %s: conflicting registration options for %s", owner, built.entry.typ)
	}
	if built.opts.contributedSet {
		if built.contributionToken == nil {
			return fmt.Errorf("module %s: As requires a contract token", owner)
		}
		key, err := keyOf(built.contributionToken)
		if err != nil {
			return err
		}
		if err := requireCanonicalToken(key, built.contributionToken.Type()); err != nil {
			return fmt.Errorf("module %s: %w", owner, err)
		}
		contractType := built.contributionToken.Type()
		if contractType.Kind() != reflect.Interface {
			return fmt.Errorf("module %s: As target %s must be an interface contract", owner, key.name)
		}
		if !implementsOrExact(built.entry.typ, contractType) {
			return fmt.Errorf("module %s: As(%s): constructor output %s does not implement %s", owner, key.name, built.entry.typ, contractType)
		}
		built.entry.contract = key
	}
	if existing, ok := c.entries[built.entry.key]; ok {
		return fmt.Errorf("service %s is registered by module %s and module %s", built.entry.key.name, existing.owner, owner)
	}
	c.entries[built.entry.key] = built.entry
	return nil
}

type builtConstructor struct {
	entry             *entry
	opts              *addOptions
	contributionToken di.Token
}

func (c *composition) buildConstructor(owner string, constructor any, opts []AddOption) (*builtConstructor, error) {
	fn := reflect.ValueOf(constructor)
	if !fn.IsValid() || fn.Kind() != reflect.Func || fn.IsNil() {
		return nil, fmt.Errorf("module %s: constructor must be a non-nil function", owner)
	}
	ft := fn.Type()
	if ft.IsVariadic() {
		return nil, fmt.Errorf("module %s: constructor %s must not be variadic", owner, ft)
	}
	if n := ft.NumOut(); n == 0 || n > 3 {
		return nil, fmt.Errorf("module %s: constructor %s must return the service value, optionally (T, error) or (T, cleanup, error)", owner, ft)
	}
	out := ft.Out(0)
	if out.Kind() == reflect.Interface {
		return nil, fmt.Errorf("module %s: constructor %s returns interface %s; constructors must return concrete values", owner, ft, out)
	}
	hasCleanup := false
	errIndex := -1
	switch ft.NumOut() {
	case 2:
		if ft.Out(1) != errorType {
			return nil, fmt.Errorf("module %s: constructor %s second return value must be error", owner, ft)
		}
		errIndex = 1
	case 3:
		if !isCleanupSignature(ft.Out(1)) {
			return nil, fmt.Errorf("module %s: constructor %s second return value must be func(context.Context) error", owner, ft)
		}
		if ft.Out(2) != errorType {
			return nil, fmt.Errorf("module %s: constructor %s third return value must be error", owner, ft)
		}
		hasCleanup, errIndex = true, 2
	}
	opts0 := &addOptions{lifetime: di.Transient}
	for _, o := range opts {
		if o == nil {
			return nil, fmt.Errorf("module %s: nil registration option", owner)
		}
		o(opts0)
	}
	var declaredKeys []tokenKey
	var tokenDeps []di.Token
	seen := map[tokenKey]bool{}
	hasCtx := false
	for i := 0; i < ft.NumIn(); i++ {
		pt := ft.In(i)
		if pt == ctxType {
			if hasCtx {
				return nil, fmt.Errorf("module %s: constructor %s takes at most one context.Context", owner, ft)
			}
			hasCtx = true
			continue
		}
		token := canonicalTokenFor(pt)
		key, err := keyOf(token)
		if err != nil {
			return nil, err
		}
		if !seen[key] {
			seen[key] = true
			declaredKeys = append(declaredKeys, key)
			tokenDeps = append(tokenDeps, token)
		}
	}
	factory, err := constructorFactory(constructor, ft, errIndex, hasCleanup)
	if err != nil {
		return nil, err
	}
	return &builtConstructor{
		entry: &entry{
			key: tokenKey{canonicalTokenName(out), out}, token: canonicalTokenFor(out), typ: out,
			owner: owner, lifetime: opts0.lifetime, declaredKeys: declaredKeys,
			tokenDeps: tokenDeps, factory: factory, isCtor: true,
		},
		opts: opts0, contributionToken: opts0.contributed,
	}, nil
}

func (c *composition) replaceConstructorLocked(owner string, constructor any, reason string, opts []AddOption) error {
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("module %s: constructor replacement requires a reason", owner)
	}
	built, err := c.buildConstructor(owner, constructor, opts)
	if err != nil {
		return err
	}
	existing, ok := c.entries[built.entry.key]
	if !ok || !existing.isCtor {
		return fmt.Errorf("module %s: Replace requires a prior registration producing %s", owner, built.entry.typ)
	}
	if built.opts.conflict {
		return fmt.Errorf("module %s: conflicting registration options for %s", owner, built.entry.typ)
	}
	previousOwner := existing.owner
	if existing.owner != owner {
		// Cross-module replacement is only legal for an exported public
		// service identity: the replacer must directly depend on the export
		// owner; unexported (private) foreign services are never replaceable.
		export, exported := c.exported[built.entry.key]
		if !exported || (owner != export && !c.modules[owner].direct[export]) {
			return fmt.Errorf("module %s: Replace requires owning %s (registered by module %s); cross-module replacement requires an exported service and a direct dependency on its export owner module %s", owner, built.entry.typ, existing.owner, export)
		}
	}
	existing.lifetime, existing.declaredKeys, existing.tokenDeps = built.entry.lifetime, built.entry.declaredKeys, built.entry.tokenDeps
	existing.factory = built.entry.factory
	// The implementation source moves to the replacer for provenance and
	// factory-dependency checks; the token and export identity are unchanged.
	existing.owner = owner
	if previousOwner != owner {
		c.logLocked("module %s replaced service %s (previous source module %s, export owner %s): %s", owner, existing.key.name, previousOwner, c.exported[existing.key], reason)
	} else {
		c.logLocked("module %s replaced %s: %s", owner, existing.key.name, reason)
	}
	return nil
}

// replaceContractLocked swaps the implementation of an exported contract with
// a different concrete constructor. The caller is either the contract owner
// or a module that directly depends on the contract owner (consumers never
// depend on the implementation module); an explicit existing implementation
// (prior contributions, owner-local candidates, or a low-level provider under
// the exact contract identity) is required — the first contribution of an
// implementation is Add(..., As(...)). The original contract owner and export
// identity are preserved, the new source module and reason are recorded, and
// ALL previous candidates of this contract are excluded for THIS contract
// only: a concrete implementing several interfaces keeps serving every other
// contract and stays resolvable as a service.
func (c *composition) replaceContractLocked(owner string, contract di.Token, constructor any, reason string, opts []AddOption) error {
	key, err := keyOf(contract)
	if err != nil {
		return err
	}
	if err := requireCanonicalToken(key, contract.Type()); err != nil {
		return fmt.Errorf("module %s: %w", owner, err)
	}
	def, ok := c.contracts[key]
	if !ok {
		return fmt.Errorf("module %s: ReplaceContract requires an exported contract %s", owner, key.name)
	}
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("module %s: ReplaceContract of %s requires a reason", owner, key.name)
	}
	if owner != def.owner && !c.modules[owner].direct[def.owner] {
		return fmt.Errorf("module %s: ReplaceContract for contract %s requires a direct dependency on the contract owner module %s", owner, key.name, def.owner)
	}
	for _, o := range opts {
		if o == nil {
			return fmt.Errorf("module %s: nil registration option", owner)
		}
	}
	built, err := c.buildConstructor(owner, constructor, opts)
	if err != nil {
		return err
	}
	if built.opts.contributedSet {
		return fmt.Errorf("module %s: ReplaceContract contribution is implicit; do not pass As for %s", owner, key.name)
	}
	if built.opts.conflict {
		return fmt.Errorf("module %s: conflicting registration options for %s", owner, built.entry.typ)
	}
	if !implementsOrExact(built.entry.typ, contract.Type()) {
		return fmt.Errorf("module %s: ReplaceContract implementation %s does not implement %s", owner, built.entry.typ, contract.Type())
	}
	var contributions []*entry
	var ownerLocal []*entry
	direct, directExists := c.entries[key]
	for _, e := range c.sortedEntriesLocked() {
		if e.contract == key {
			contributions = append(contributions, e)
			continue
		}
		if e.contract == zeroTokenKey && e.isCtor && e.owner == def.owner && implementsOrExact(e.typ, def.typ) {
			ownerLocal = append(ownerLocal, e)
		}
	}
	if len(contributions) == 0 && len(ownerLocal) == 0 && !(directExists && !direct.isCtor) {
		return fmt.Errorf("module %s: ReplaceContract of %s requires an existing implementation to replace; declare the first contribution with Add(..., As(%s))", owner, key.name, key.name)
	}
	oldOwner := "<none>"
	previousTypes := make([]string, 0, len(contributions)+len(ownerLocal)+1)
	for _, e := range contributions {
		if oldOwner == "<none>" {
			oldOwner = e.owner
		}
		previousTypes = append(previousTypes, e.typ.String())
		e.contract = zeroTokenKey
		e.excludeContract(key)
	}
	for _, e := range ownerLocal {
		if oldOwner == "<none>" {
			oldOwner = e.owner
		}
		previousTypes = append(previousTypes, e.typ.String())
		e.excludeContract(key)
	}
	if directExists && !direct.isCtor && oldOwner == "<none>" {
		oldOwner = direct.owner
		previousTypes = append(previousTypes, direct.typ.String())
	}
	if existing, ok := c.entries[built.entry.key]; ok {
		if !existing.excluded[key] {
			return fmt.Errorf("service %s is registered by module %s and module %s", built.entry.key.name, existing.owner, owner)
		}
		// The replacement re-uses the identity of an excluded previous
		// implementation of the same contract; the new entry takes the slot.
		delete(c.entries, built.entry.key)
	}
	built.entry.contract = key
	c.entries[built.entry.key] = built.entry
	c.logLocked("module %s replaced contract %s implementations [%s] (module %s) with %s owned by module %s, contract owner %s unchanged: %s", owner, key.name, strings.Join(previousTypes, ", "), oldOwner, built.entry.typ, owner, def.owner, reason)
	return nil
}

func constructorFactory(constructor any, ft reflect.Type, errIndex int, hasCleanup bool) (di.Factory, error) {
	fn := reflect.ValueOf(constructor)
	var hasCtx bool
	ctxIndex := -1
	for i := 0; i < ft.NumIn(); i++ {
		if ft.In(i) == ctxType {
			hasCtx, ctxIndex = true, i
		}
	}
	paramTokens := make([]di.Token, 0, ft.NumIn())
	for i := 0; i < ft.NumIn(); i++ {
		if ft.In(i) == ctxType {
			continue
		}
		paramTokens = append(paramTokens, canonicalTokenFor(ft.In(i)))
	}
	if !hasCtx {
		return func(ctx context.Context, resolver di.Resolver) (any, di.Cleanup, error) {
			args, err := resolveArgsWithoutCtx(ctx, resolver, paramTokens, ft)
			if err != nil {
				return nil, nil, err
			}
			return finishCall(fn.Call(args), errIndex, hasCleanup, ft)
		}, nil
	}
	return func(ctx context.Context, resolver di.Resolver) (any, di.Cleanup, error) {
		args, err := resolveArgs(ctx, resolver, paramTokens, ft, ctxIndex)
		if err != nil {
			return nil, nil, err
		}
		return finishCall(fn.Call(args), errIndex, hasCleanup, ft)
	}, nil
}

func resolveArgs(ctx context.Context, resolver di.Resolver, tokens []di.Token, ft reflect.Type, ctxIndex int) ([]reflect.Value, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	args := make([]reflect.Value, 0, ft.NumIn())
	ti := 0
	for i := 0; i < ft.NumIn(); i++ {
		if i == ctxIndex {
			args = append(args, reflect.ValueOf(ctx))
			continue
		}
		v, err := resolver.Resolve(ctx, tokens[ti])
		ti++
		if err != nil {
			return nil, fmt.Errorf("constructor %s: %w", ft, err)
		}
		args = append(args, reflect.ValueOf(v))
	}
	return args, nil
}

func resolveArgsWithoutCtx(ctx context.Context, resolver di.Resolver, tokens []di.Token, ft reflect.Type) ([]reflect.Value, error) {
	args := make([]reflect.Value, 0, ft.NumIn())
	for _, token := range tokens {
		v, err := resolver.Resolve(ctx, token)
		if err != nil {
			return nil, fmt.Errorf("constructor %s: %w", ft, err)
		}
		args = append(args, reflect.ValueOf(v))
	}
	return args, nil
}

func finishCall(out []reflect.Value, errIndex int, hasCleanup bool, ft reflect.Type) (any, di.Cleanup, error) {
	var cleanup di.Cleanup
	if hasCleanup && !isNilValue(out[1]) {
		cleanupFn := out[1]
		cleanup = func(cctx context.Context) error {
			if cctx == nil {
				cctx = context.Background()
			}
			res := cleanupFn.Call([]reflect.Value{reflect.ValueOf(cctx)})
			if isNilValue(res[0]) {
				return nil
			}
			err, _ := res[0].Interface().(error)
			return err
		}
	}
	if errIndex >= 0 && !isNilValue(out[errIndex]) {
		if err, ok := out[errIndex].Interface().(error); ok && err != nil {
			return nil, nil, err
		}
	}
	_ = ft
	return out[0].Interface(), cleanup, nil
}

func isCleanupSignature(t reflect.Type) bool {
	return t.Kind() == reflect.Func && t.NumIn() == 1 && t.In(0) == ctxType && t.NumOut() == 1 && t.Out(0) == errorType
}

var (
	ctxType   = reflect.TypeFor[context.Context]()
	errorType = reflect.TypeFor[error]()
)

func isNilValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

// Seal validates the composition deterministically and hands out the sealed
// di container. Validation and di graph provisioning run under the
// composition lock; the actual di.Seal (which eagerly constructs user
// singletons, including their factories and cleanups) runs BEFORE the lock
// is re-taken, so a constructor can never observe a composition state and a
// revoked registrar fails promptly instead of deadlocking.
func (c *composition) Seal(ctx context.Context) (di.Container, error) {
	c.mu.Lock()
	if c.sealed {
		c.mu.Unlock()
		return nil, fmt.Errorf("composition is sealed")
	}
	if ctx == nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("composition context is required")
	}
	c.sealed = true
	providers, err := c.provisionLocked()
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	registry, err := di.NewRegistry(c.cleanupTimeout)
	if err != nil {
		return nil, err
	}
	for _, p := range providers {
		if err := registry.Provide(p); err != nil {
			return nil, err
		}
	}
	container, err := registry.Seal(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.container = container
	c.mu.Unlock()
	return container, nil
}

// provisionLocked runs every engine-level validation and returns the provider
// list for the di registry without touching it. It executes no user code.
func (c *composition) provisionLocked() ([]di.Provider, error) {
	resolved, err := c.resolveContractsLocked()
	if err != nil {
		return nil, err
	}
	aliases, err := c.resolvePrivateAliasesLocked()
	if err != nil {
		return nil, err
	}
	c.privateAliases = aliases
	if err := c.checkLaunderingLocked(resolved, aliases); err != nil {
		return nil, err
	}
	if err := c.checkVisibilityLocked(aliases); err != nil {
		return nil, err
	}
	providers := make([]di.Provider, 0, len(c.entries)+len(c.contracts)+len(aliases))
	for _, def := range c.sortedContractsLocked() {
		r := resolved[defKey(def)]
		if r == nil || r.direct != nil || r.selected == nil {
			continue // low-level identity or missing implementation (reported above)
		}
		sel := r.selected
		providers = append(providers, di.Provider{
			Key:       def.token,
			Lifetime:  sel.lifetime,
			DependsOn: []di.Token{sel.token},
			Factory: func(ctx context.Context, resolver di.Resolver) (any, di.Cleanup, error) {
				v, err := resolver.Resolve(ctx, sel.token)
				if err != nil {
					return nil, nil, err
				}
				return v, nil, nil
			},
		})
	}
	for _, e := range c.sortedEntriesLocked() {
		depends, factory := e.tokenDeps, e.factory
		// Constructor parameters and low-level declared dependencies both
		// translate through the same guarded alias table so validation and
		// provisioning agree. The table is built from declared keys only:
		// an undeclared factory resolver access stays raw and is rejected
		// by the di factory guard, and custom-named tokens never carry a
		// canonical parameter identity so they always resolve exactly.
		if len(aliases) > 0 {
			subst := make([]di.Token, len(e.tokenDeps))
			wrapped := false
			for i, dep := range e.tokenDeps {
				id, idErr := keyOf(dep)
				if idErr != nil {
					return nil, idErr
				}
				subst[i] = dep
				if _, isAlias := aliases[privateAliasKey{owner: e.owner, param: id}]; isAlias {
					subst[i] = aliasToken(privateAliasKey{owner: e.owner, param: id})
					wrapped = true
				}
			}
			depends = subst
			if wrapped {
				table := map[tokenKey]di.Token{}
				for i, dep := range e.tokenDeps {
					id, idErr := keyOf(dep)
					if idErr != nil {
						return nil, idErr
					}
					if _, isAlias := aliases[privateAliasKey{owner: e.owner, param: id}]; isAlias {
						table[id] = subst[i]
					}
				}
				factory = privateParamFactory(e.factory, table)
			}
		}
		providers = append(providers, di.Provider{Key: e.token, Lifetime: e.lifetime, DependsOn: depends, Factory: factory})
	}
	for _, aliasKey := range sortedPrivateAliasKeys(aliases) {
		providers = append(providers, privateAliasProvider(aliases[aliasKey]))
	}
	return providers, nil
}

func defKey(def *contractDef) tokenKey { return tokenKey{def.token.Name(), def.typ} }

type resolvedContract struct {
	def        *contractDef
	direct     *entry
	selected   *entry
	candidates []*entry
}

func (c *composition) resolveContractsLocked() (map[tokenKey]*resolvedContract, error) {
	// Private bindings recorded before a later Export follow the public rule
	// once the key is claimed; registration order must not change outcomes.
	if err := c.promotePrivateBindingsLocked(); err != nil {
		return nil, err
	}
	// Binding targets must exist before any candidate resolution; bindings on
	// concrete-exported tokens are reported as redundant in applyResolutions.
	for _, key := range sortedBindingKeys(c.bindings) {
		if _, ok := c.contracts[key]; ok {
			continue
		}
		if _, concrete := c.exported[key]; concrete {
			continue
		}
		return nil, fmt.Errorf("module %s binds contract %s: no module exported this contract", c.bindings[key].owner, key.name)
	}
	resolved := map[tokenKey]*resolvedContract{}
	for _, def := range c.sortedContractsLocked() {
		resolved[defKey(def)] = &resolvedContract{def: def}
	}
	// Binding owner legality and candidate enumeration.
	for _, def := range c.sortedContractsLocked() {
		key := defKey(def)
		r := resolved[key]
		if e, ok := c.entries[key]; ok {
			if e.owner != def.owner {
				return nil, fmt.Errorf("module %s registers a low-level provider for contract %s owned by module %s", e.owner, key.name, def.owner)
			}
			r.direct = e
		}
		for _, e := range c.sortedEntriesLocked() {
			if e.excluded[key] {
				continue
			}
			if e.key == key {
				// The canonical low-level provider IS the contract's single
				// implementation (recorded as r.direct above); it must not
				// additionally count as an owner-local structural candidate
				// of itself.
				continue
			}
			// Owner-local structural inference considers only CONSTRUCTOR
			// registrations or explicit As contributions; low-level named
			// providers are resolvable only by their exact token identity
			// and never become implicit interface candidates.
			if e.contract == key || (e.contract == zeroTokenKey && e.isCtor && e.owner == def.owner && implementsOrExact(e.typ, def.typ)) {
				r.candidates = append(r.candidates, e)
			}
		}
		if r.direct != nil {
			if len(r.candidates) > 0 {
				return nil, fmt.Errorf("contract %s is implemented by both a low-level provider and constructor candidates (module %s)", key.name, r.candidates[0].owner)
			}
			if _, bound := c.bindings[key]; bound {
				return nil, fmt.Errorf("redundant binding for contract %s: exactly one low-level implementation exists", key.name)
			}
		}
		for _, cand := range r.candidates {
			if cand.owner == def.owner {
				continue
			}
			if !c.modules[cand.owner].direct[def.owner] {
				return nil, fmt.Errorf("module %s contributes %s to contract owned by module %s without declaring a direct dependency on the owner", cand.owner, cand.token.Name(), def.owner)
			}
		}
	}
	if err := c.applyResolutionsLocked(resolved); err != nil {
		return nil, err
	}
	return resolved, nil
}

func (c *composition) applyResolutionsLocked(resolved map[tokenKey]*resolvedContract) error {
	for _, def := range c.sortedContractsLocked() {
		key := defKey(def)
		r := resolved[key]
		binding := c.bindings[key]
		if len(r.candidates) == 0 && r.direct == nil {
			return fmt.Errorf("contract %s exported by module %s has no implementation", key.name, def.owner)
		}
		if r.direct != nil {
			continue
		}
		switch len(r.candidates) {
		case 1:
			if binding != nil {
				return fmt.Errorf("redundant binding for contract %s: candidate %s from module %s is the only implementation", key.name, r.candidates[0].typ, r.candidates[0].owner)
			}
			r.selected = r.candidates[0]
		default:
			if binding == nil {
				return fmt.Errorf("ambiguous contract %s owned by module %s: %s", key.name, def.owner, candidateListLocked(r.candidates))
			}
			if _, exported := c.exported[key]; !exported {
				return fmt.Errorf("contract %s owned by module %s cannot be resolved: the owner has not exported it", key.name, def.owner)
			}
			if binding.owner != def.owner && !c.modules[binding.owner].direct[def.owner] {
				return fmt.Errorf("module %s: binding for contract %s owned by module %s is restricted to the contract owner or a module directly depending on the contract owner", binding.owner, key.name, def.owner)
			}
			var match *entry
			for _, cand := range r.candidates {
				if cand.typ == binding.typ {
					match = cand
					break
				}
			}
			if match == nil {
				return fmt.Errorf("module %s: binding for contract %s does not match any candidate (%s)", binding.owner, key.name, candidateListLocked(r.candidates))
			}
			// The contract owner is always allowed to resolve the ambiguity
			// on its own contract; any other binder must directly depend on
			// both the contract owner and the implementation owner.
			if binding.owner != def.owner && binding.owner != match.owner && !c.modules[binding.owner].direct[match.owner] {
				return fmt.Errorf("module %s: binding for contract %s selects implementation of module %s without a direct dependency on it; only the contract owner or a module directly depending on both the contract owner and the implementation owner may bind", binding.owner, key.name, match.owner)
			}
			r.selected = match
		}
	}
	// Bindings on keys that are not exported interface contracts.
	for _, key := range sortedBindingKeys(c.bindings) {
		if _, ok := c.contracts[key]; ok {
			continue
		}
		b := c.bindings[key]
		if owner, ok := c.exported[key]; ok {
			return fmt.Errorf("redundant binding for contract %s: exactly one exported implementation by module %s", key.name, owner)
		}
		return fmt.Errorf("module %s binds contract %s: no module exported this contract", b.owner, key.name)
	}
	return nil
}

func candidateListLocked(cands []*entry) string {
	parts := make([]string, 0, len(cands))
	for _, cand := range cands {
		parts = append(parts, fmt.Sprintf("module=%s type=%s", cand.owner, cand.typ))
	}
	return strings.Join(parts, "; ")
}

// checkLaunderingLocked rejects Singleton graphs that reach an Operation
// service through any edge, including Transient laundering, public contract
// delegations and constructor parameters resolved through owner-private
// aliases. Private alias edges mirror the runtime factory graph exactly:
// a consumer depends on its alias, and the alias carries the selected
// implementation's lifetime.
func (c *composition) checkLaunderingLocked(resolved map[tokenKey]*resolvedContract, aliases map[privateAliasKey]*privateAlias) error {
	type node struct {
		lifetime di.Lifetime
		deps     []tokenKey
		owner    string
	}
	nodes := map[tokenKey]node{}
	for key, e := range c.entries {
		nodes[key] = node{e.lifetime, append([]tokenKey(nil), e.declaredKeys...), e.owner}
	}
	for _, aliasKey := range sortedPrivateAliasKeys(aliases) {
		alias := aliases[aliasKey]
		aliasId := tokenKey{name: aliasName(aliasKey), typ: aliasKey.param.typ}
		for _, e := range c.sortedEntriesLocked() {
			if e.owner != aliasKey.owner {
				continue
			}
			consumes := false
			for _, dep := range e.declaredKeys {
				if dep == aliasKey.param {
					consumes = true
					break
				}
			}
			if !consumes {
				continue
			}
			n := nodes[e.key]
			n.deps = append(n.deps, aliasId)
			nodes[e.key] = n
		}
		nodes[aliasId] = node{alias.selected.lifetime, []tokenKey{alias.selected.key}, aliasKey.owner}
	}
	for _, def := range c.sortedContractsLocked() {
		r := resolved[defKey(def)]
		if r == nil || r.direct != nil || r.selected == nil {
			continue
		}
		sel := r.selected
		nodes[defKey(def)] = node{sel.lifetime, []tokenKey{sel.key}, def.owner}
	}
	clean := map[tokenKey]bool{}
	var visit func(key tokenKey, owner string, path []string, onPath map[tokenKey]bool) error
	visit = func(key tokenKey, owner string, path []string, onPath map[tokenKey]bool) error {
		if clean[key] || onPath[key] {
			return nil
		}
		n, ok := nodes[key]
		if !ok {
			return nil
		}
		if n.lifetime == di.Operation {
			chain := strings.Join(append(append([]string(nil), path...), key.name), " -> ")
			return fmt.Errorf("singleton %s (module %s) captures operation %s: %s", path[0], owner, key.name, chain)
		}
		onPath[key] = true
		path = append(path, key.name)
		for _, dep := range n.deps {
			if err := visit(dep, owner, path, onPath); err != nil {
				return err
			}
		}
		delete(onPath, key)
		clean[key] = true
		return nil
	}
	for _, rootKey := range sortedEntryKeys(c.entries) {
		n := nodes[rootKey]
		if n.lifetime != di.Singleton {
			continue
		}
		if err := visit(rootKey, n.owner, nil, map[tokenKey]bool{}); err != nil {
			return err
		}
	}
	for _, def := range c.sortedContractsLocked() {
		r := resolved[defKey(def)]
		if r == nil || r.direct != nil || r.selected == nil || r.selected.lifetime != di.Singleton {
			continue
		}
		if err := visit(defKey(def), def.owner, nil, map[tokenKey]bool{}); err != nil {
			return err
		}
	}
	return nil
}

func (c *composition) checkVisibilityLocked(aliases map[privateAliasKey]*privateAlias) error {
	for _, e := range c.sortedEntriesLocked() {
		for _, dep := range e.declaredKeys {
			if _, private := aliases[privateAliasKey{owner: e.owner, param: dep}]; private {
				continue // resolved through the owner-private alias
			}
			if err := c.visibilityLocked(e.owner, dep); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *composition) visibilityLocked(consumer string, dep tokenKey) error {
	if strings.Contains(dep.name, privateAliasMarker) {
		return fmt.Errorf("module %s: dependency %s is an engine-managed private alias identity: private aliases cannot be declared as provider dependencies or resolved as tokens", consumer, dep.name)
	}
	owner, ok := c.ownerOfTokenLocked(dep)
	if !ok {
		return fmt.Errorf("module %s: missing provider %s (no module registers it; interface contracts additionally require an Export by their owner)", consumer, dep.name)
	}
	if owner == consumer {
		return nil
	}
	if !c.modules[consumer].direct[owner] || c.exported[dep] != owner {
		return fmt.Errorf("module %s cannot resolve %s: it is owned by module %s; declare a direct dependency on %s and require it to export %s", consumer, dep.name, owner, owner, dep.name)
	}
	return nil
}

func (c *composition) ownerOfTokenLocked(key tokenKey) (string, bool) {
	// The export/contract owner is immutable: replacements move only the
	// implementation source, Consumers keep resolving through the export
	// identity.
	if export, ok := c.exported[key]; ok {
		return export, true
	}
	if e, ok := c.entries[key]; ok {
		return e.owner, true
	}
	if def, ok := c.contracts[key]; ok {
		return def.owner, true
	}
	return "", false
}

// LifecycleResolver returns the restricted post-seal resolver of a module.
func (c *composition) LifecycleResolver(owner string) (di.Resolver, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.sealed || c.container == nil {
		return nil, fmt.Errorf("composition is not sealed")
	}
	if _, ok := c.modules[owner]; !ok {
		return nil, fmt.Errorf("composition has no module %s", owner)
	}
	return &lifecycleResolver{c: c, owner: owner}, nil
}

// lifecycleResolver is deliberately unexported: module Start/Stop callbacks
// never receive the container itself. Resolve checks the module's visibility
// and then delegates to the raw container. No ephemeral operation scope is
// opened and no returned resource is disposed here; Operation-timed services
// therefore require an explicit scoped resolution by the caller.
type lifecycleResolver struct {
	c     *composition
	owner string
}

func (r *lifecycleResolver) Resolve(ctx context.Context, key di.Token) (any, error) {
	id, err := keyOf(key)
	if err != nil {
		return nil, err
	}
	if contextIsNil(ctx) {
		return nil, fmt.Errorf("DI context is required")
	}
	r.c.mu.Lock()
	if _, ok := r.c.modules[r.owner]; !ok {
		// The owner module disappeared from the plan (or never existed):
		// treated as not sealed-visible.
		r.c.mu.Unlock()
		return nil, fmt.Errorf("composition has no module %s", r.owner)
	}
	if !r.c.sealed || r.c.container == nil {
		r.c.mu.Unlock()
		return nil, fmt.Errorf("composition is not sealed")
	}
	container := r.c.container
	// The owner resolves its own private interface parameter tokens through
	// the engine alias table or, for a unique lifecycle-only implementation,
	// through the dynamic own-provider selection; every other path keeps the
	// public visibility rules and stays blind to foreign private aliases and
	// alias identities.
	tok, handled, err := r.c.restrictedResolveTokenLocked(r.owner, key, id)
	if err != nil {
		r.c.mu.Unlock()
		return nil, err
	}
	if !handled {
		if vErr := r.c.visibilityLocked(r.owner, id); vErr != nil {
			r.c.mu.Unlock()
			return nil, vErr
		}
	}
	r.c.mu.Unlock()
	return container.Resolve(ctx, tok)
}

// ReplacementLog records provenance-bearing replacements in registration
// order: constructor Replace, low-level Replace, binding and contract
// implementation swaps.
func (c *composition) ReplacementLog() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.replacementLog...)
}

func (c *composition) logLocked(format string, args ...any) {
	c.replacementLog = append(c.replacementLog, fmt.Sprintf(format, args...))
}

func (c *composition) sortedEntriesLocked() []*entry {
	entries := make([]*entry, 0, len(c.entries))
	for _, e := range c.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return lessTokenKey(entries[i].key, entries[j].key) })
	return entries
}

func (c *composition) sortedContractsLocked() []*contractDef {
	defs := make([]*contractDef, 0, len(c.contracts))
	for _, def := range c.contracts {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return lessTokenKey(defKey(defs[i]), defKey(defs[j])) })
	return defs
}

func sortedEntryKeys(m map[tokenKey]*entry) []tokenKey {
	keys := make([]tokenKey, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return lessTokenKey(keys[i], keys[j]) })
	return keys
}

func sortedBindingKeys(m map[tokenKey]*binding) []tokenKey {
	keys := make([]tokenKey, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return lessTokenKey(keys[i], keys[j]) })
	return keys
}

func lessTokenKey(a, b tokenKey) bool {
	if a.name != b.name {
		return a.name < b.name
	}
	// Identity stays reflect.Type; the display string sorts deterministically
	// so same-basename types from different packages keep distinct identity.
	return a.typ.String() < b.typ.String()
}

type keyListResult struct {
	tokens []di.Token
	keys   []tokenKey
}

func keyList(tokens []di.Token) (keyListResult, error) {
	out := keyListResult{tokens: append([]di.Token(nil), tokens...)}
	seen := map[tokenKey]bool{}
	for _, t := range tokens {
		key, err := keyOf(t)
		if err != nil {
			return out, err
		}
		if seen[key] {
			return out, fmt.Errorf("duplicate dependency %s", key.name)
		}
		seen[key] = true
		out.keys = append(out.keys, key)
	}
	return out, nil
}

func implementsOrExact(concrete reflect.Type, contract reflect.Type) bool {
	if concrete == contract {
		return true
	}
	return contract.Kind() == reflect.Interface && concrete.Implements(contract)
}

func requireCanonicalToken(key tokenKey, typ reflect.Type) error {
	if key.name != canonicalTokenName(typ) {
		return fmt.Errorf("token %q must be the canonical token of %s; custom-named tokens cannot own contracts (use module.Contract[T]() / module.Service[T]())", key.name, typ)
	}
	return nil
}
