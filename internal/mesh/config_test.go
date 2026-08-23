package mesh

import "testing"

func TestMeshIsDisabledByDefault(t *testing.T) {
	t.Setenv("BACKFLASH_MESH_ENABLED", "")
	if DefaultConfig().Enabled || Load().Enabled {
		t.Fatal("mesh ska vara avstängt som standard")
	}
}

func TestMeshRequiresExplicitOptIn(t *testing.T) {
	t.Setenv("BACKFLASH_MESH_ENABLED", "1")
	if !Load().Enabled {
		t.Fatal("explicit mesh-opt-in ignorerades")
	}
}
