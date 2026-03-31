package templates

import (
	"context"
	"html"
	"html/template"

	"careme/internal/seasons"
)

const (
	aboutAlbumImageBaseURL  = "https://images.northbriton.net/"
	aboutAlbumPreviewPrefix = "https://images.northbriton.net/cdn-cgi/image/width=800/https://images.northbriton.net/"
)

type AboutAlbumPhoto struct {
	Comment    string
	ImageID    string
	RecipeHash string
}

func (p AboutAlbumPhoto) FullURL() string {
	return aboutAlbumImageBaseURL + p.ImageID
}

func (p AboutAlbumPhoto) PreviewURL() string {
	return aboutAlbumPreviewPrefix + p.ImageID
}

func (p AboutAlbumPhoto) CommentHTML() template.HTML {
	if p.Comment == "" {
		return ""
	}
	return template.HTML("<!-- " + html.EscapeString(p.Comment) + " -->")
}

type AboutPageData struct {
	ClarityScript   template.HTML
	GoogleTagScript template.HTML
	Style           seasons.Style
	AlbumPhotos     []AboutAlbumPhoto
}

func NewAboutPageData(ctx context.Context, style seasons.Style) AboutPageData {
	return AboutPageData{
		ClarityScript:   ClarityScript(ctx),
		GoogleTagScript: GoogleTagScript(),
		Style:           style,
		AlbumPhotos:     DefaultAboutAlbumPhotos(),
	}
}

func DefaultAboutAlbumPhotos() []AboutAlbumPhoto {
	return append([]AboutAlbumPhoto(nil), defaultAboutAlbumPhotos...)
}

var defaultAboutAlbumPhotos = []AboutAlbumPhoto{
	{Comment: "Dungeness crab pasta", ImageID: "AP1GczMBNFe2Ol-2vq1uZybQcd1y5P1vu4jqNbbtX4U0uvc_GSIlszulZjtIzGIxtgEm6hHPoPLOV8BqzDyMdSzSl4qCGyTlV2fSyyYnq_ipUEREpthJs6Uf", RecipeHash: "SNsa7tJ414rhmrw56fjqiw=="},
	{Comment: "tri-tip and polenta", ImageID: "AP1GczPKKoL9ZZ3nmrcjf0d6tgLazTkNk2Yii2YfVhaRJoTA9Aap264ABsKEm5cqFJwKByELDuiMFas_z0KyjjvBRmEHL_yRHrTTIXbdT-7jKX1mqFKKejAp", RecipeHash: "le76qpHPhb-EK2KB7TqGZA=="},
	{Comment: "Tomato Paprika Chicken thighs", ImageID: "AP1GczO7v4JPGuK09-1TPLFz0SpUL2XghE8lxK_5Sn153pRGc_DlBZhslyqCOd3vfibyqCw6eWLca3-DHj0zFChi4rJBpeQuHOqFOdThjhj81KnngZ-Fs0Fy", RecipeHash: "Z-EeQGRhSw5549Xh5L4Y3w=="},
	{Comment: "tri tip pear balsamic", ImageID: "AP1GczPLfs_p6MyK8mTldz-L0vUUaot9JilVEsj7UFpxLjNDq2DmF1P7O79TJwB9Ov9D1XxKpwnfGevG4Wi8ZgFBqblOz7-O8O0ZvxEcgeaX056_Hlq8dlYr", RecipeHash: "wgWTsu-0-pB_ox3acbolwg=="},
	{Comment: "lent miso salmon and slaw", ImageID: "AP1GczORLaBqbZOpJjmBC4_BeHV05HuNZHarGgRIkSdASAC6vrmezVMy3gxbxi-uxMw5pZevgXsnoVWRiB461qEXi3DS__lajYIjsi9G1v5gx3dGoq2P5_5X", RecipeHash: "fAe4BgOZgMpFyDwwazT9FQ=="},
	{Comment: "sausage kale pasta", ImageID: "AP1GczODcc6U8rXC6v9NcbUhHuYRa3JYqRxZsn6flSVCwfGL6_C1BNrzFpwfzaj29hg7QVFOomYxLoskLWJnHcBZqHe4FhDg83JFSGYPycvvTMCbgdBugu58", RecipeHash: "t3BmgENY9ipTu-OD4RY3XA=="},
	{Comment: "tri-tip steak and chimichurri but don't have hash have to go search", ImageID: "AP1GczPNp90QZFqrjkBSfhDHHDd-32Cabii-NX0CcmFiTnB4pXZXMFGhiDR3kj20DOGYI_uuCXt80fudQB0Zuf9yfiSdOLhULcJxn8UfH9xrm0I8BiCUB_Ej"},
	{Comment: "lamb kofta also don't have hash", ImageID: "AP1GczP5HTwdWhDCTB_lbeVC8fnFJzsGo41qveO3baMUv-kRLBM53QI2nrgJe0ZmvxJsePdjjZN1XJN-EbgjyeAcjO8gDlq2Al6WqXCtK8R7HMzaVEwwSSIM"},
	{Comment: "lamb!", ImageID: "AP1GczPmHb7lMdUyBz3yo_MAqc9kjYGoKKJf3vQYCXujbmU7Tg9_Gjoc-PdtPi0jxdW5_HImLrBZBCqWhgDMFAKM7lMUAJonq4SN4XDal5lIFfZJMFQ4oNY7"},
	{Comment: "salmon with potatoes and broccolini", ImageID: "AP1GczN1MlLNlY09U_gPcbp30FdyaJlkTS53ayx4f-fJL_zjwoJlKg2wWlmcUyCV-GopCebXHCooxXwZfaWgVTRLqKV916OTUm2-Bpy0oMN5uTwyudLsF7sS", RecipeHash: "M_HC2ROGixCf6_KNbWFk9w=="},
	{Comment: "Roasted Maple Dijon Salmon with Potatoes and Green Beans no hash", ImageID: "AP1GczPhyKs35SqVF4GlHF9-XHr_yi7hZys8ePOIXauKghpH6TGNx53RM67_Evx8TAabzq2mYcWJ6W2CfmVRpY99wplUa60MDZvFLlTeY6YS3h8BIJEAipaE"},
	{Comment: "squash stuffed with sausage and kale", ImageID: "AP1GczOJwYRFtwp1dd0qCsPZmOXFfYQOxHCv1vOnQtUAxKVyKoqkNJencWU3tzkSB_HR7046NN-jST5n1FsFHm5nCHkKsFANtXJ8yb8dBq7qGvVWsdWst0Dd", RecipeHash: "mQs4oIYMJoqCmqDMXv74bA=="},
	{Comment: "Salmon carrots and fennel", ImageID: "AP1GczPDySERu2MtYUCR5ltT0XXUpF2482sMjgVN6qd3SyiICx2euO5yIuj0QgwFl9nqy3tGLe_-OUqIpuKjY1J5RVC7RWz10TOrhtaz5meJn6z1hxD1p6wt", RecipeHash: "K5rSNX2zMuxzwLKTZgsSRg=="},
	{Comment: "ribeye and asparagus", ImageID: "AP1GczP33uH4Pxdg2EotHBWZLxnJ7zfJMB6Nz82x4_PlOpl-ruS1s0ylIeiwVxLqHSZhadsdjDOUD32R9QGWfQkVO3kXAf9-OiXp7HFo70Bs1aKpT81LWpik", RecipeHash: "OYX56jveSjqi4XmcInbBwQ=="},
	{Comment: "ginger chicken bok choy", ImageID: "AP1GczPov-6n9v5K6jQzLLcXsJtQJMCqD7o4s3DZd_bRIIf4lsgpme6d8BJY93MswT9aoaNroeqS9HAPRYI1TmOp3pKD_xfSLOF9-Nkba-WdsPIsAQsemYHG"},
	{Comment: "kristen chicken and squash", ImageID: "AP1GczN_NQn7tQXHgD5l0kUHzlb-5KlY8riCtx8VYy5uIZq2w-qoMeuVCgT0DMm_gdeQxP0mgFbuRYNoWYUs6i6gFdZWn2QBoLbBrAdvGDqonE2RmYlSTzlF"},
	{Comment: "chicken slow cooker", ImageID: "AP1GczOimx3VaB16JNvZh_szWEFddQnupyOoEDon4791Wl3D799H13wR8kR3yX0lpDy7DXN-yNCQnFoMB_iBFKLgJF7J2dO9xKvrpTAp40xSJMoshzertOYb", RecipeHash: "IOEiITiU68CSwHDPf5yNCA=="},
	{Comment: "slow cooked chicken in pot", ImageID: "AP1GczMh7l7aY778MCqQBaHVCBaE80T0zI6nx62-Q7U75mAa-Z11rhgAf8woEYYgfbvL6UDLSBMjPDgzh6TRk1C7P1zfosOnjnMjR1su5zkoEQ_Y3px44oEg", RecipeHash: "IOEiITiU68CSwHDPf5yNCA=="},
	{Comment: "LAMB pan", ImageID: "AP1GczMHg8W9L-iaK16Prwc0AB2jpryvQ3V_RB8dKWyC_NM9FaNzn-HravSYsMwfzIKtUFn_dyxR-zXVjOOdFqOREUnjqHXrkFjgEgwiDdb1-MrHA87vNNTm", RecipeHash: "cbycxIAij_RK6vD4BfptFQ=="},
	{Comment: "half eaten lamb chopps", ImageID: "AP1GczOMjRr7tmZ5xxPzXsUHOip34t32QHfZJrQFsj6bo_FeU3P38DoRsHP-iAqxZMgj_WPE3XiJRMpDM6ezi8f8Q1pd1d92EnFacQF-vkgy6qv2ULgct8qh", RecipeHash: "cbycxIAij_RK6vD4BfptFQ=="},
	{Comment: "chicken thights fennel and carrots", ImageID: "AP1GczO4qkM5zaXym17qH8Cy2IpWW_SHdWDmkKMiRx_VcN4ZBG9_dwI3ybDdri2v8n9XFNdCprnv72kD2JCwMnSkz38Mqa95OORDDjppLMGimj0DLbQATOf3", RecipeHash: "7AvK-N9pE6lJY0S40JK23A=="},
	{Comment: "sausage, mushrooms and kale pasta", ImageID: "AP1GczMxEf7tY7cpxOuJnHlzJw48xq-JtP_x5XVjNCzs8m_a6HuizPEVgjWKsuVs84WNwa181arukeILhn32Lx6u_XwjDamwRUMnVylxChG8i7K_-fK56ztG", RecipeHash: "HZsnsGnH739VEKUrE18KGg=="},
}
