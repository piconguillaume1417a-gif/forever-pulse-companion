// mkres produit cmd/forever-pulse-companion/rsrc_windows_amd64.syso : icône de
// l'exécutable, informations de version (« Forever Pulse Companion ») et manifeste
// (contrôles communs v6 : boîtes de dialogue et infobulles modernes ; DPI système ;
// sans élévation).
//
//	go run ./outils/mkres 0.6.4
package main

import (
	"fmt"
	"image"
	"os"

	"wowsync/internal/icone"
	"wowsync/outils/mkres/winres"
	"wowsync/outils/mkres/winres/version"
)

func main() {
	ver := "0.6.4"
	if len(os.Args) > 1 {
		ver = os.Args[1]
	}
	var imgs []image.Image
	for _, s := range []int{16, 20, 24, 32, 40, 48, 64, 256} {
		imgs = append(imgs, icone.Dessine(s, icone.SansPastille))
	}
	icon, err := winres.NewIconFromImages(imgs)
	if err != nil {
		panic(err)
	}
	rs := &winres.ResourceSet{}
	if err := rs.SetIcon(winres.ID(1), icon); err != nil {
		panic(err)
	}
	var vi version.Info
	vi.SetFileVersion(ver)
	vi.SetProductVersion(ver)
	for k, v := range map[string]string{
		version.ProductName: "Forever Pulse Companion", version.FileDescription: "Forever Pulse Companion",
		version.InternalName: "ForeverPulseCompanion", version.OriginalFilename: "ForeverPulseCompanion.exe",
		version.CompanyName: "Forever Pulse",
		version.Comments:    "Envoie les relevés de l'addon Forever Pulse au site. Non affilié à Blizzard.",
	} {
		_ = vi.Set(0x040C, k, v)
	}
	rs.SetVersionInfo(vi)
	rs.SetManifest(winres.AppManifest{
		Identity:       winres.AssemblyIdentity{Name: "ForeverPulse.Companion", Version: [4]uint16{0, 2, 0, 0}},
		Description:    "Forever Pulse Companion",
		DPIAwareness:   winres.DPIAware,
		ExecutionLevel: winres.AsInvoker,
		Compatibility:  winres.Win7AndAbove,
		LongPathAware:  true,
		// 0.6.0 : TaskDialogIndirect et les infobulles demandent comctl32 v6.
		UseCommonControlsV6: true,
	})
	out := "cmd/forever-pulse-companion/rsrc_windows_amd64.syso"
	f, err := os.Create(out)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := rs.WriteObject(f, winres.ArchAMD64); err != nil {
		panic(err)
	}
	fmt.Println("écrit :", out)
}
