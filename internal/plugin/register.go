package plugin

// registerAllPlugins 注册所有支付插件
// 由 InitAll() 调用，只执行一次
func registerAllPlugins() {
	// ===== 支付宝系列插件 =====

	// 8个主要插件
	Register(NewAlipayFacePlugin())     // alipay_face_to - 当面付
	Register(NewAlipayQrPlugin())       // alipay_ddm - 扫码支付
	Register(NewAlipayFaceJsPlugin())   // alipay_jsapi - 当面付JS
	Register(NewAlipayWapPlugin())      // alipay_wap - 手机网站支付
	Register(NewAlipayPhonePlugin())    // alipay_phone - 手机网站支付(直接)
	Register(NewAlipayPhone2Plugin())   // alipay_phone2 - 手机网站支付(OAuth)
	Register(NewAlipayPcPlugin())       // alipay_pc - 电脑网站支付
	Register(NewAlipayAppPlugin())      // alipay_app - APP支付

	// 7个子目录插件
	Register(NewAlipayGoldPlugin())       // alipay_gold - 黄金UID
	Register(NewAlipayFaceQrPlugin())     // alipay_face_qr - 当面付QR
	Register(NewAlipayCardUidPlugin())    // alipay_card_uid - 名片UID
	Register(NewAlipayConfirmUidPlugin()) // alipay_confirm - 确认单UID
	Register(NewAlipayPrePlugin())        // alipay_ysq - 预授权
	Register(NewAlipayJsXcxPlugin())      // alipay_jsxcx - JS小程序
	Register(NewAlipayC2cHongBaoPlugin()) // alipay_c2c - C2C红包
}
