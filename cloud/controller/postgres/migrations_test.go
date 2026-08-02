package postgres

import "testing"

func TestMigrationChecksums(t *testing.T) {
	t.Parallel()

	want := map[int]string{
		1: "664355186faa2462b6f1bf5e8a9251c91489613d6fbe88c42b3607478db301ec",
		2: "100b07ac5772aaf20641315f360811754390c9ecba842e99969d163f3a4b4fcc",
		3: "777dfda0d36de32170dad9cda8a195e9718fde824d6f76657075c2d4309029a6",
		4: "4674afd48367520eb820956e9bc16062887eebfe22c33ea10a958de605c4d06a",
		5: "71d330e7303db35031fb41e11864820effbc8e4b118a98237753321475a1b813",
		6: "25ae44bc9a383b0825fc6dc658d6777e63add6b3decb2b1952565eee98e4fcee",
		7: "e03cea7a2a098f5cc2d62cd7a973f5f40fc9afda8feeb5ce9560bbe51bbe7f51",
		8: "a556ae097dc095f1fb0162d23b4f0b917ddae37208916a209307bb1548e5e9af",
		9: "f1e0db3833d45fdacee78780cc61630fa9f2374c0498573ff9ab88a1fba80f12",
	}

	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != len(want) {
		t.Fatalf("got %d migrations, want %d", len(migrations), len(want))
	}
	for _, item := range migrations {
		if item.checksum != want[item.version] {
			t.Errorf("migration %d checksum = %s, want %s", item.version, item.checksum, want[item.version])
		}
	}
}
