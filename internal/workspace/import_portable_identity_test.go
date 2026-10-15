package workspace

import (
	"archive/zip"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func orderedPortablePreview(t *testing.T, k *Kernel, entries map[string]string, csvFirst bool) ImportPlan {
	t.Helper()
	name := filepath.Join(t.TempDir(), "ordered.zip")
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	names := []string{}
	for name := range entries {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := strings.HasSuffix(names[i], ".csv"), strings.HasSuffix(names[j], ".csv")
		if a != b {
			return a == csvFirst
		}
		return names[i] < names[j]
	})
	for _, name := range names {
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(entries[name])); e != nil {
			t.Fatal(e)
		}
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	handle, err := k.RegisterSelection(name)
	if err != nil {
		t.Fatal(err)
	}
	v, err := previewPortableImport(context.Background(), k, k.OwnerContext(), map[string]any{"sourceHandle": handle, "sourceNamespace": "typed-merge"})
	if err != nil {
		t.Fatal(err)
	}
	return v.(ImportPlan)
}
func TestPortableCanonicalRenditionsRemainStableAcrossArchiveOrder(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, csvFirst := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{false: "modern", true: "legacy"}[legacy], map[bool]string{false: "markdown_first", true: "csv_first"}[csvFirst]}, "/"), func(t *testing.T) {
				source, se := testKernel(t)
				f := newDatabase(t, source, se)
				_, _ = newRecord(t, source, se, f, "Stable record")
				entries := exportPrivacyEntries(t, source, se, false)
				target, te := testKernel(t)
				first := completeImportFixture(t, target, te, orderedPortablePreview(t, target, entries, csvFirst))
				expected := map[string]ImportItem{}
				for _, item := range first.Items {
					if item.Kind == "markdown" || item.Kind == "portable_collection" {
						expected[item.Locator] = item
					}
				}
				if legacy {
					// Emulate the historical order-dependent address without changing source,
					// local page identity, or the known retained normalized plan evidence.
					for i := range first.Items {
						item := &first.Items[i]
						if _, ok := expected[item.Locator]; !ok {
							continue
						}
						kind, id := importIdentity(item.Locator)
						if item.Kind == "portable_collection" && !csvFirst {
							kind, id = "derived_locator", item.Locator
						}
						// Find owner via collection rather than relying on optional page identity map.
						if item.Kind == "markdown" && csvFirst {
							for _, c := range first.PortableCollections {
								if item.PageID == c.PageID {
									kind, id = "derived_locator", item.Locator
								}
							}
						}
						if _, err := target.db.Exec("UPDATE import_mappings SET identity_kind=?,identity_value=? WHERE id=?", kind, id, item.MappingID); err != nil {
							t.Fatal(err)
						}
						item.IdentityKind, item.ExternalID = kind, id
					}
					if err := target.write(context.Background(), func(tx *sql.Tx) error { return saveImportPlan(context.Background(), tx, first) }); err != nil {
						t.Fatal(err)
					}
				}
				second := orderedPortablePreview(t, target, entries, !csvFirst)
				for _, item := range second.Items {
					if old, ok := expected[item.Locator]; ok {
						if item.PageID != old.PageID || item.MappingID != old.MappingID {
							t.Fatalf("rendition changed canonical mapping: %s", marshal(item))
						}
					}
				}
				second = reviewDatabasePreview(t, target, second, nil)
				completeImportFixture(t, target, te, second)
				var count int
				target.db.QueryRow("SELECT count(*) FROM collections").Scan(&count)
				if count != 1 {
					t.Fatal("duplicated collection", count)
				}
			})
		}
	}
}
