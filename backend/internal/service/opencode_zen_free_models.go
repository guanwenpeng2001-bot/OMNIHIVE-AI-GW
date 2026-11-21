package service

func openCodeZenFreeModelPresets() []PlatformModelPreset {
	p := freeLanePreset(PlatformOpenCodeGo, "opencode-zen-free", "our-free-model/vendor/zen-free/models.mjs")
	for _, row := range [][2]string{
		{"big-pickle", "Big Pickle"}, {"mimo-v2.6-flash-free", "MiMo V2.6 Flash Free"}, {"deepseek-v4.1-flash-free", "DeepSeek V4.1 Flash Free"}, {"longcat-2.5-free", "LongCat 2.5 Free"}, {"iceman-free", "Iceman Free"}, {"beam-free", "Beam Free"}, {"flint-40b-free", "Flint 40B Free"}, {"nemotron-3.5-super-vl-free", "Nemotron 3.5 Super VL Free"}, {"nemotron-3.5-lightning-free", "Nemotron 3.5 Lightning Free"}, {"hy4-flash-free", "HY4 Flash Free"}, {"tidepool-alpha-free", "Tidepool Alpha Free"}, {"space-bunny-free", "Space Bunny Free"}, {"muse-spark-1.2-contributor-free", "Muse Spark 1.2 Contributor Free"}, {"muse-spark-1.3-contributor-free", "Muse Spark 1.3 Contributor Free"}, {"union-alpha", "Union Alpha"},
	} {
		m := freeLanePresetModel(row[0], row[1], p.SourceRefs[0])
		if row[0] == "big-pickle" {
			m.ContextWindow = 200000
			m.MaxInputTokens = 160000
			m.MaxOutputTokens = 32000
		}
		p.Models = append(p.Models, m)
	}
	return []PlatformModelPreset{p}
}
