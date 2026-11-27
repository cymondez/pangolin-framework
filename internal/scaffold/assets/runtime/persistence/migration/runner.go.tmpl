package migration

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

var ErrIncompatible = errors.New("SCHEMA_INCOMPATIBLE")

// PostgresRunner runs deployment SQL migrations. CheckCompatibility only reads.
// Source may be an embed.FS, a joined FS, or os.DirFS(repositoryRoot).
type PostgresRunner struct {
	db               *sql.DB
	source           fs.FS
	minimum, maximum int64
}

func NewPostgres(db *sql.DB, source fs.FS, minimum, maximum int64) (*PostgresRunner, error) {
	if db == nil || source == nil || minimum < 0 || maximum < minimum {
		return nil, errors.New("invalid migration configuration")
	}
	return &PostgresRunner{db: db, source: source, minimum: minimum, maximum: maximum}, nil
}

func (r *PostgresRunner) Apply(ctx context.Context, modules []ModuleMigrations) (returned error) {
	if ctx == nil {
		return errors.New("migration context required")
	}
	files, err := collect(r.source, modules)
	if err != nil {
		return err
	}
	// Goose Up calls HasPending before its configured session locker. Hold the
	// deployment lock around the complete operation, including schema/version-table
	// initialization. The dedicated lock session is never given to business code.
	if r.db.Stats().MaxOpenConnections == 1 {
		return errors.New("migration pool requires at least two connections")
	}
	owned, err := (PostgresLocker{DB: r.db}).Acquire(ctx, LockKey)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		returned = errors.Join(returned, owned.Release(cleanup))
	}()
	if _, err = owned.(*sessionLock).conn.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS pangolin"); err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, r.db, files, goose.WithTableName("pangolin.goose_db_version"))
	if err != nil {
		return err
	}
	_, err = provider.Up(ctx)
	return err
}

func (r *PostgresRunner) CheckCompatibility(ctx context.Context) error {
	if ctx == nil {
		return ErrIncompatible
	}
	var table sql.NullString
	if err := r.db.QueryRowContext(ctx, "SELECT to_regclass('pangolin.goose_db_version')::text").Scan(&table); err != nil {
		return errors.Join(ErrIncompatible, err)
	}
	if !table.Valid {
		return ErrIncompatible
	}
	var current int64
	// Down migrations are deliberately absent from this forward-only runner.
	if err := r.db.QueryRowContext(ctx, "SELECT COALESCE(max(version_id),0) FROM pangolin.goose_db_version WHERE is_applied").Scan(&current); err != nil {
		return errors.Join(ErrIncompatible, err)
	}
	if current < r.minimum || current > r.maximum {
		return fmt.Errorf("%w: version %d outside [%d,%d]", ErrIncompatible, current, r.minimum, r.maximum)
	}
	return nil
}

// PostgresLocker implements the deployment Port with an exclusively owned session.
type PostgresLocker struct{ DB *sql.DB }

func (l PostgresLocker) Acquire(ctx context.Context, key int64) (Lock, error) {
	if ctx == nil || l.DB == nil {
		return nil, errors.New("migration lock requires database and context")
	}
	locker, err := newSessionLocker(key)
	if err != nil {
		return nil, err
	}
	conn, err := l.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	if err = locker.SessionLock(ctx, conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &sessionLock{conn: conn, locker: locker}, nil
}

type sessionLock struct {
	conn   *sql.Conn
	locker *safeSessionLocker
	once   sync.Once
	err    error
}

func (l *sessionLock) Release(ctx context.Context) error {
	l.once.Do(func() {
		if ctx == nil {
			ctx = context.Background()
		}
		l.err = l.locker.SessionUnlock(ctx, l.conn)
		l.err = errors.Join(l.err, l.conn.Close())
	})
	return l.err
}

type safeSessionLocker struct {
	underlying lock.SessionLocker
}

func newSessionLocker(key int64) (*safeSessionLocker, error) {
	l, err := lock.NewPostgresSessionLocker(lock.WithLockID(key), lock.WithLockTimeout(1, 60), lock.WithUnlockTimeout(1, 2))
	return &safeSessionLocker{underlying: l}, err
}
func discard(conn *sql.Conn) { _ = conn.Raw(func(any) error { return driver.ErrBadConn }) }
func (l *safeSessionLocker) SessionLock(ctx context.Context, conn *sql.Conn) error {
	err := l.underlying.SessionLock(ctx, conn)
	if err != nil {
		discard(conn)
	}
	return err
}
func (l *safeSessionLocker) SessionUnlock(ctx context.Context, conn *sql.Conn) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	err := l.underlying.SessionUnlock(cleanup, conn)
	if err != nil {
		discard(conn)
	}
	return err
}

// JoinFS mounts package-owned embedded migration files without copying them.
func JoinFS(sources map[string]fs.FS) fs.FS {
	copySources := make(map[string]fs.FS, len(sources))
	for name, source := range sources {
		copySources[name] = source
	}
	return joinedFS{copySources}
}

type joinedFS struct{ sources map[string]fs.FS }

func (j joinedFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}
	if name == "." {
		names := make([]string, 0, len(j.sources))
		for mount := range j.sources {
			names = append(names, mount)
		}
		sort.Strings(names)
		return &bundleDir{names: names, dirEntries: true}, nil
	}
	first, rest, ok := strings.Cut(name, "/")
	if !ok {
		rest = "."
	}
	source := j.sources[first]
	if source == nil {
		return nil, fs.ErrNotExist
	}
	file, err := source.Open(rest)
	if err != nil {
		return nil, err
	}
	if rest == "." {
		return mountedRoot{File: file, name: first}, nil
	}
	return file, nil
}

type mountedRoot struct {
	fs.File
	name string
}

func (f mountedRoot) Stat() (fs.FileInfo, error) { return fileInfo{name: f.name, dir: true}, nil }
func (f mountedRoot) ReadDir(n int) ([]fs.DirEntry, error) {
	directory, ok := f.File.(fs.ReadDirFile)
	if !ok {
		return nil, fs.ErrInvalid
	}
	return directory.ReadDir(n)
}

type bundleFS struct{ files map[string][]byte }

func collect(source fs.FS, modules []ModuleMigrations) (*bundleFS, error) {
	byName := map[string]ModuleMigrations{}
	for _, m := range modules {
		if strings.TrimSpace(m.Module) == "" || m.Directory == "" {
			return nil, errors.New("migration module and directory required")
		}
		if _, ok := byName[m.Module]; ok {
			return nil, fmt.Errorf("duplicate migration module %s", m.Module)
		}
		byName[m.Module] = m
	}
	ordered := []ModuleMigrations{}
	marks := map[string]int{}
	var visit func(string) error
	visit = func(name string) error {
		if marks[name] == 1 {
			return fmt.Errorf("migration dependency cycle at %s", name)
		}
		if marks[name] == 2 {
			return nil
		}
		m, ok := byName[name]
		if !ok {
			return fmt.Errorf("missing migration dependency %s", name)
		}
		marks[name] = 1
		deps := append([]string(nil), m.DependsOn...)
		sort.Strings(deps)
		for _, dep := range deps {
			if err := visit(dep); err != nil {
				return err
			}
		}
		marks[name] = 2
		ordered = append(ordered, m)
		return nil
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	files := &bundleFS{files: map[string][]byte{}}
	versions := map[int64]string{}
	maxima := map[string]int64{}
	for _, m := range ordered {
		directory, err := fs.ReadDir(source, m.Directory)
		if err != nil {
			return nil, fmt.Errorf("migration directory %s: %w", m.Module, err)
		}
		count := 0
		var minimum, maximum int64
		for _, entry := range directory {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
				continue
			}
			prefix, _, ok := strings.Cut(entry.Name(), "_")
			version, err := strconv.ParseInt(prefix, 10, 64)
			if !ok || err != nil || version <= 0 {
				return nil, fmt.Errorf("invalid migration filename %s", entry.Name())
			}
			if previous := versions[version]; previous != "" {
				return nil, fmt.Errorf("duplicate migration version %d in %s and %s", version, previous, m.Module)
			}
			raw, err := fs.ReadFile(source, path.Join(m.Directory, entry.Name()))
			if err != nil {
				return nil, err
			}
			files.files[entry.Name()] = raw
			versions[version] = m.Module
			count++
			if minimum == 0 || version < minimum {
				minimum = version
			}
			if version > maximum {
				maximum = version
			}
		}
		if count == 0 {
			return nil, fmt.Errorf("no SQL migrations for %s", m.Module)
		}
		for _, dep := range m.DependsOn {
			if minimum <= maxima[dep] {
				return nil, fmt.Errorf("migration versions for %s precede dependency %s", m.Module, dep)
			}
		}
		maxima[m.Module] = maximum
	}
	if len(files.files) == 0 {
		return nil, errors.New("migration modules required")
	}
	return files, nil
}
func (b *bundleFS) Open(name string) (fs.File, error) {
	if name == "." {
		names := make([]string, 0, len(b.files))
		for name := range b.files {
			names = append(names, name)
		}
		sort.Strings(names)
		return &bundleDir{names: names, files: b.files}, nil
	}
	raw, ok := b.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &bundleFile{Reader: bytes.NewReader(raw), name: name}, nil
}

type bundleFile struct {
	*bytes.Reader
	name string
}

func (f *bundleFile) Close() error               { return nil }
func (f *bundleFile) Stat() (fs.FileInfo, error) { return fileInfo{name: f.name, size: f.Size()}, nil }

type fileInfo struct {
	name string
	size int64
	dir  bool
}

func (f fileInfo) Name() string { return f.name }
func (f fileInfo) Size() int64  { return f.size }
func (f fileInfo) Mode() fs.FileMode {
	if f.dir {
		return fs.ModeDir | 0555
	}
	return 0444
}
func (f fileInfo) ModTime() time.Time { return time.Time{} }
func (f fileInfo) IsDir() bool        { return f.dir }
func (f fileInfo) Sys() any           { return nil }

type bundleDir struct {
	names      []string
	files      map[string][]byte
	offset     int
	dirEntries bool
}

func (d *bundleDir) Close() error               { return nil }
func (d *bundleDir) Stat() (fs.FileInfo, error) { return fileInfo{name: ".", dir: true}, nil }
func (d *bundleDir) Read([]byte) (int, error)   { return 0, fs.ErrInvalid }
func (d *bundleDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.offset == len(d.names) && n > 0 {
		return nil, io.EOF
	}
	end := len(d.names)
	if n > 0 && d.offset+n < end {
		end = d.offset + n
	}
	result := make([]fs.DirEntry, 0, end-d.offset)
	for _, name := range d.names[d.offset:end] {
		result = append(result, fs.FileInfoToDirEntry(fileInfo{name: name, size: int64(len(d.files[name])), dir: d.dirEntries}))
	}
	d.offset = end
	return result, nil
}

// Filesystem returns a migration source rooted at the caller's repository path.
func Filesystem(directory string) fs.FS { return os.DirFS(directory) }
