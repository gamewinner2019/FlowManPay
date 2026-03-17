package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/gamewinner2019/FlowManPay/internal/middleware"
	"github.com/gamewinner2019/FlowManPay/internal/model"
	"github.com/gamewinner2019/FlowManPay/internal/pkg/response"
	"github.com/gamewinner2019/FlowManPay/internal/plugin"
)

// AlipaySubRequestHandler 支付宝直付通子请求处理器
type AlipaySubRequestHandler struct {
	DB  *gorm.DB
	RDB *redis.Client
}

// NewAlipaySubRequestHandler 创建直付通子请求处理器
func NewAlipaySubRequestHandler(db *gorm.DB, rdb *redis.Client) *AlipaySubRequestHandler {
	return &AlipaySubRequestHandler{DB: db, RDB: rdb}
}

// externalIDRandom 生成外部ID
func externalIDRandom() string {
	return fmt.Sprintf("%d", time.Now().UnixMicro())
}

// getUserFilter 根据用户身份获取过滤条件（writeoff的parent_id）
// 返回 tenantID（用于 writeoff.parent_id 过滤）
func (h *AlipaySubRequestHandler) getUserFilter(c *gin.Context) (uint, error) {
	// 检查 Sub-Auth header（匿名访问）
	auth := c.GetHeader("Sub-Auth")
	if auth != "" {
		val := h.RDB.Get(c.Request.Context(), fmt.Sprintf("sub_ten_%s", auth)).Val()
		if val != "" {
			tenantID, err := strconv.ParseUint(val, 10, 64)
			if err == nil {
				return uint(tenantID), nil
			}
		}
	}

	user, exists := middleware.GetCurrentUser(c)
	if !exists {
		return 0, fmt.Errorf("没有权限")
	}

	if user.Role.Key == model.RoleKeyWriteoff {
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
			return 0, fmt.Errorf("核销不存在")
		}
		return writeoff.ParentID, nil
	}

	if user.Role.Key == model.RoleKeyTenant {
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			return 0, fmt.Errorf("租户不存在")
		}
		return tenant.ID, nil
	}

	if user.Role.Key == model.RoleKeyAdmin || user.IsSuperuser {
		return 0, nil // admin 不过滤
	}

	return 0, fmt.Errorf("没有权限")
}

// List 列表（GET /api/alipay/sub/request/）
func (h *AlipaySubRequestHandler) List(c *gin.Context) {
	page, limit, offset := response.GetPagination(c)

	tenantID, err := h.getUserFilter(c)
	if err != nil {
		response.ErrorResponse(c, err.Error())
		return
	}

	query := h.DB.Model(&model.AlipaySubProduct{})
	if tenantID > 0 {
		// 过滤 writeoff.parent_id = tenantID
		query = query.Joins("JOIN "+model.TablePrefix+"system_writeoff w ON w.id = "+model.TablePrefix+"alipay_sub_product.writeoff_id").
			Where("w.parent_id = ?", tenantID)
	}

	var total int64
	query.Count(&total)

	var items []model.AlipaySubProduct
	query.Preload("Writeoff").
		Order("create_datetime DESC").
		Offset(offset).Limit(limit).
		Find(&items)

	// 构建响应，附加 history 信息
	type subProductResp struct {
		ExternalID   string      `json:"external_id"`
		Name         *string     `json:"name"`
		MerchantType *string     `json:"merchant_type"`
		CreateTime   time.Time   `json:"create_datetime"`
		UpdateTime   time.Time   `json:"update_datetime"`
		Status       string      `json:"status"`
		WriteoffName string      `json:"writeoff_name"`
		TenantName   string      `json:"tenant_name,omitempty"`
		Reason       *string     `json:"reason,omitempty"`
	}

	user, _ := middleware.GetCurrentUser(c)
	isAdmin := user != nil && (user.Role.Key == model.RoleKeyAdmin || user.IsSuperuser)

	results := make([]subProductResp, 0, len(items))
	for _, item := range items {
		resp := subProductResp{
			ExternalID:   item.ExternalID,
			Name:         item.Name,
			MerchantType: item.MerchantType,
			CreateTime:   item.CreateDatetime,
			UpdateTime:   item.UpdateDatetime,
			Status:       "0",
		}

		// 获取最新 history
		var history model.AlipaySubProductRequestHistory
		if err := h.DB.Where("sub_merchant_id = ?", item.ExternalID).
			Order("create_datetime DESC").First(&history).Error; err == nil {
			resp.Status = history.Status
			resp.Reason = history.Reason
		}

		// 获取核销名称
		if item.Writeoff != nil {
			var writeoffUser model.Users
			if err := h.DB.First(&writeoffUser, item.Writeoff.SystemUserID).Error; err == nil {
				resp.WriteoffName = writeoffUser.Name
			}
			// admin 额外获取租户名称
			if isAdmin {
				var tenantUser model.Users
				var tenant model.Tenant
				if err := h.DB.First(&tenant, item.Writeoff.ParentID).Error; err == nil {
					if err := h.DB.First(&tenantUser, tenant.SystemUserID).Error; err == nil {
						resp.TenantName = tenantUser.Name
					}
				}
			}
		}

		results = append(results, resp)
	}

	response.PageResponse(c, results, total, page, limit, "")
}

// UploadImage 图片上传（POST /api/alipay/sub/request/image/upload/）
func (h *AlipaySubRequestHandler) UploadImage(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.ErrorResponse(c, "文件上传失败")
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		response.ErrorResponse(c, "文件类型错误")
		return
	}

	imageType := strings.Replace(contentType, "image/", "", 1)
	imageType = strings.Replace(imageType, "jpeg", "jpg", 1)

	// 读取文件内容
	content := make([]byte, header.Size)
	if _, err := file.Read(content); err != nil {
		response.ErrorResponse(c, "读取文件失败")
		return
	}

	indirectID := c.Query("indirect_id")
	if indirectID == "" {
		response.ErrorResponse(c, "找不到直付通账号")
		return
	}

	tenantID, err := h.getUserFilter(c)
	if err != nil {
		response.ErrorResponse(c, err.Error())
		return
	}

	// 查找直付通账号
	query := h.DB.Where("id = ? AND account_type = 5 AND status = ?", indirectID, true)
	if tenantID > 0 {
		query = query.Joins("JOIN "+model.TablePrefix+"system_writeoff w ON w.id = "+model.TablePrefix+"alipay_product.writeoff_id").
			Where("w.parent_id = ?", tenantID)
	}
	var product model.AlipayProduct
	if err := query.First(&product).Error; err != nil {
		response.ErrorResponse(c, "找不到直付通账号")
		return
	}

	sdk, err := plugin.NewAlipaySDKFromProduct(h.DB, &product)
	if err != nil {
		response.ErrorResponse(c, "SDK初始化失败: "+err.Error())
		return
	}

	res, err := sdk.IndirectImageUpload(imageType, header.Filename, content)
	if err != nil {
		response.ErrorResponse(c, "上传失败: "+err.Error())
		return
	}

	if code, _ := res["code"].(string); code != "10000" {
		msg := fmt.Sprintf("%v%v%v", res["msg"], res["sub_code"], res["sub_msg"])
		response.ErrorResponse(c, msg)
		return
	}

	response.DetailResponse(c, map[string]interface{}{
		"indirect_id": product.ID,
		"image_id":    res["image_id"],
	}, "ok")
}

// GetIndirectID 获取直付通账号列表（GET /api/alipay/sub/request/indirect/id/）
func (h *AlipaySubRequestHandler) GetIndirectID(c *gin.Context) {
	tenantID, err := h.getUserFilter(c)
	if err != nil {
		response.ErrorResponse(c, err.Error())
		return
	}

	query := h.DB.Where("account_type = 5 AND status = ? AND deleted_at IS NULL", true)
	if tenantID > 0 {
		query = query.Joins("JOIN "+model.TablePrefix+"system_writeoff w ON w.id = "+model.TablePrefix+"alipay_product.writeoff_id").
			Where("w.parent_id = ?", tenantID)
	}

	// 检查 Sub-Auth 关联的 indirect_id
	auth := c.GetHeader("Sub-Auth")
	if auth != "" {
		indirectID := h.RDB.Get(c.Request.Context(), fmt.Sprintf("sub_%s", auth)).Val()
		if indirectID != "" {
			query = query.Where(model.TablePrefix+"alipay_product.id = ?", indirectID)
		}
	}

	var products []model.AlipayProduct
	query.Select("id, name").Find(&products)

	type simpleProduct struct {
		ID   uint   `json:"id"`
		Name string `json:"name"`
	}
	results := make([]simpleProduct, 0, len(products))
	for _, p := range products {
		results = append(results, simpleProduct{ID: p.ID, Name: p.Name})
	}

	response.DetailResponse(c, results, "success")
}

// IndirectCreate 创建/保存进件草稿（POST /api/alipay/sub/request/indirect/:indirect_id/draft/）
func (h *AlipaySubRequestHandler) IndirectCreate(c *gin.Context) {
	indirectID, _ := strconv.Atoi(c.Param("indirect_id"))
	if indirectID == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误: "+err.Error())
		return
	}

	externalID, _ := req["external_id"].(string)
	if externalID == "" {
		externalID = externalIDRandom()
	}

	tenantID, err := h.getUserFilter(c)
	if err != nil {
		response.ErrorResponse(c, err.Error())
		return
	}

	// 确定 writeoff_id
	var writeoffID uint
	auth := c.GetHeader("Sub-Auth")
	user, userExists := middleware.GetCurrentUser(c)

	if !userExists && auth != "" {
		val := h.RDB.Get(c.Request.Context(), fmt.Sprintf("sub_wri_%s", auth)).Val()
		if val != "" {
			wid, _ := strconv.ParseUint(val, 10, 64)
			writeoffID = uint(wid)
		} else {
			response.ErrorResponse(c, "没有权限")
			return
		}
	} else if userExists && user.Role.Key == model.RoleKeyWriteoff {
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
			response.ErrorResponse(c, "核销不存在")
			return
		}
		writeoffID = writeoff.ID
	} else if wid, ok := req["writeoff"]; ok {
		switch v := wid.(type) {
		case float64:
			writeoffID = uint(v)
		case string:
			id, _ := strconv.ParseUint(v, 10, 64)
			writeoffID = uint(id)
		}
	}

	// 查找直付通账号
	productQuery := h.DB.Where("id = ? AND account_type = 5 AND status = ?", indirectID, true)
	if tenantID > 0 {
		productQuery = productQuery.Joins("JOIN "+model.TablePrefix+"system_writeoff w ON w.id = "+model.TablePrefix+"alipay_product.writeoff_id").
			Where("w.parent_id = ?", tenantID)
	}
	var aliProduct model.AlipayProduct
	if err := productQuery.First(&aliProduct).Error; err != nil {
		response.ErrorResponse(c, "找不到直付通账号")
		return
	}

	// 检查是否已存在
	var existing model.AlipaySubProduct
	found := h.DB.Where("external_id = ?", externalID).First(&existing).Error == nil

	if found && existing.Smid != nil {
		response.ErrorResponse(c, "该商户已经成功进件")
		return
	}

	if found {
		// 权限检查
		if !userExists && auth != "" {
			var writeoff model.WriteOff
			if err := h.DB.First(&writeoff, existing.WriteoffID).Error; err == nil {
				if fmt.Sprintf("%d", tenantID) != fmt.Sprintf("%d", writeoff.ParentID) {
					response.ErrorResponse(c, "进件不存在")
					return
				}
			}
		} else if userExists && user.Role.Key == model.RoleKeyTenant {
			var writeoff model.WriteOff
			if err := h.DB.First(&writeoff, existing.WriteoffID).Error; err == nil {
				var tenant model.Tenant
				if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err == nil {
					if tenant.ID != writeoff.ParentID {
						response.ErrorResponse(c, "进件不存在")
						return
					}
				}
			}
		}

		// 更新已有记录
		h.updateSubProduct(&existing, req, uint(indirectID), writeoffID)
		h.DB.Save(&existing)
		response.DetailResponse(c, map[string]interface{}{
			"external_id": existing.ExternalID,
		}, "保存成功")
		return
	}

	// 创建新记录
	subProduct := model.AlipaySubProduct{
		ExternalID: externalID,
		IndirectID: uint(indirectID),
		WriteoffID: writeoffID,
	}
	h.updateSubProduct(&subProduct, req, uint(indirectID), writeoffID)
	if err := h.DB.Create(&subProduct).Error; err != nil {
		response.ErrorResponse(c, "创建失败: "+err.Error())
		return
	}

	response.DetailResponse(c, map[string]interface{}{
		"external_id": subProduct.ExternalID,
	}, "保存成功")
}

// updateSubProduct 从请求数据更新 SubProduct 字段
func (h *AlipaySubRequestHandler) updateSubProduct(sp *model.AlipaySubProduct, data map[string]interface{}, indirectID, writeoffID uint) {
	sp.IndirectID = indirectID
	if writeoffID > 0 {
		sp.WriteoffID = writeoffID
	}

	if v, ok := data["name"].(string); ok {
		sp.Name = &v
	}
	if v, ok := data["alias_name"].(string); ok {
		sp.AliasName = &v
	}
	if v, ok := data["merchant_type"].(string); ok {
		sp.MerchantType = &v
	}
	if v, ok := data["mcc"].(string); ok {
		sp.Mcc = &v
	}
	if v, ok := data["cert_no"].(string); ok {
		sp.CertNo = &v
	}
	if v, ok := data["cert_type"].(string); ok {
		sp.CertType = &v
	}
	if v, ok := data["cert_image"].(string); ok {
		sp.CertImage = &v
	}
	if v, ok := data["cert_image_back"].(string); ok {
		sp.CertImageBack = &v
	}
	if v, ok := data["legal_name"].(string); ok {
		sp.LegalName = &v
	}
	if v, ok := data["legal_cert_no"].(string); ok {
		sp.LegalCertNo = &v
	}
	if v, ok := data["alipay_logon_id"].(string); ok {
		sp.AlipayLogonID = &v
	}
	if v, ok := data["binding_alipay_logon_id"].(string); ok {
		sp.BindingAlipayLogonID = &v
	}
	if v, ok := data["legal_cert_back_image"].(string); ok {
		sp.LegalCertBackImage = &v
	}
	if v, ok := data["legal_cert_front_image"].(string); ok {
		sp.LegalCertFrontImage = &v
	}
	if v, ok := data["license_auth_letter_image"].(string); ok {
		sp.LicenseAuthLetterImage = &v
	}
	if v, ok := data["service_phone"].(string); ok {
		sp.ServicePhone = &v
	}
	if v, ok := data["sign_time_with_isv"].(string); ok {
		sp.SignTimeWithIsv = &v
	}
	if v, ok := data["cert_name"].(string); ok {
		sp.CertName = &v
	}
	if v, ok := data["legal_cert_type"].(string); ok {
		sp.LegalCertType = &v
	}
	if v, ok := data["merchant_nature"].(string); ok {
		sp.MerchantNature = &v
	}

	// JSON 字段 — 通过 marshal+unmarshal 转为正确的自定义类型
	if v, ok := data["contact_infos"]; ok {
		if b, err := json.Marshal(v); err == nil {
			var s model.JSONSlice
			if json.Unmarshal(b, &s) == nil {
				sp.ContactInfos = s
			}
		}
	}
	if v, ok := data["biz_cards"]; ok {
		if b, err := json.Marshal(v); err == nil {
			var s model.JSONSlice
			if json.Unmarshal(b, &s) == nil {
				sp.BizCards = s
			}
		}
	}
	if v, ok := data["service"]; ok {
		if b, err := json.Marshal(v); err == nil {
			var s model.JSONSlice
			if json.Unmarshal(b, &s) == nil {
				sp.Service = s
			}
		}
	}
	if v, ok := data["default_settle_rule"]; ok {
		if b, err := json.Marshal(v); err == nil {
			var m model.JSONMap
			if json.Unmarshal(b, &m) == nil {
				sp.DefaultSettleRule = m
			}
		}
	}
	if v, ok := data["business_address"]; ok {
		if b, err := json.Marshal(v); err == nil {
			var m model.JSONMap
			if json.Unmarshal(b, &m) == nil {
				sp.BusinessAddress = m
			}
		}
	}
	if v, ok := data["invoice_info"]; ok {
		if b, err := json.Marshal(v); err == nil {
			var m model.JSONMap
			if json.Unmarshal(b, &m) == nil {
				sp.InvoiceInfo = m
			}
		}
	}
	if v, ok := data["out_door_images"]; ok {
		if b, err := json.Marshal(v); err == nil {
			var s model.JSONSlice
			if json.Unmarshal(b, &s) == nil {
				sp.OutDoorImages = s
			}
		}
	}
	if v, ok := data["in_door_images"]; ok {
		if b, err := json.Marshal(v); err == nil {
			var s model.JSONSlice
			if json.Unmarshal(b, &s) == nil {
				sp.InDoorImages = s
			}
		}
	}
	if v, ok := data["sites"]; ok {
		if b, err := json.Marshal(v); err == nil {
			var s model.JSONSlice
			if json.Unmarshal(b, &s) == nil {
				sp.Sites = s
			}
		}
	}
	if v, ok := data["qualifications"]; ok {
		if b, err := json.Marshal(v); err == nil {
			var s model.JSONSlice
			if json.Unmarshal(b, &s) == nil {
				sp.Qualifications = s
			}
		}
	}
}

// IndirectQuery 查询进件状态（POST /api/alipay/sub/request/indirect/:indirect_id/query/）
func (h *AlipaySubRequestHandler) IndirectQuery(c *gin.Context) {
	indirectID, _ := strconv.Atoi(c.Param("indirect_id"))
	if indirectID == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}

	tenantID, err := h.getUserFilter(c)
	if err != nil {
		response.ErrorResponse(c, err.Error())
		return
	}

	// 查找直付通账号
	productQuery := h.DB.Where("id = ? AND account_type = 5 AND status = ?", indirectID, true)
	if tenantID > 0 {
		productQuery = productQuery.Joins("JOIN "+model.TablePrefix+"system_writeoff w ON w.id = "+model.TablePrefix+"alipay_product.writeoff_id").
			Where("w.parent_id = ?", tenantID)
	}
	var aliProduct model.AlipayProduct
	if err := productQuery.First(&aliProduct).Error; err != nil {
		response.ErrorResponse(c, "找不到直付通账号")
		return
	}

	var req struct {
		OrderID    string `json:"order_id"`
		ExternalID string `json:"external_id"`
		Writeoff   uint   `json:"writeoff"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	// 确定 writeoff_id
	var writeoffID uint
	auth := c.GetHeader("Sub-Auth")
	user, userExists := middleware.GetCurrentUser(c)

	if !userExists && auth != "" {
		val := h.RDB.Get(c.Request.Context(), fmt.Sprintf("sub_wri_%s", auth)).Val()
		if val != "" {
			wid, _ := strconv.ParseUint(val, 10, 64)
			writeoffID = uint(wid)
		} else {
			response.ErrorResponse(c, "没有权限")
			return
		}
	} else if userExists && user.Role.Key == model.RoleKeyWriteoff {
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err == nil {
			writeoffID = writeoff.ID
		}
	} else {
		writeoffID = req.Writeoff
	}

	sdk, err := plugin.NewAlipaySDKFromProduct(h.DB, &aliProduct)
	if err != nil {
		response.ErrorResponse(c, "SDK初始化失败")
		return
	}

	res, err := sdk.IndirectZftOrderQuery(req.OrderID, req.ExternalID)
	if err != nil {
		response.ErrorResponse(c, "查询失败: "+err.Error())
		return
	}

	log.Printf("IndirectQuery response: %v", res)

	if code, _ := res["code"].(string); code != "10000" {
		errMsg := fmt.Sprintf("%v,%v,%v", res["code"], res["sub_msg"], res["sub_code"])
		response.ErrorResponse(c, errMsg)
		return
	}

	orders, _ := res["orders"].([]interface{})
	count := 0
	existCount := 0

	for _, orderRaw := range orders {
		order, ok := orderRaw.(map[string]interface{})
		if !ok {
			continue
		}
		status, _ := order["status"].(string)
		if status != "99" {
			continue
		}
		smid, _ := order["smid"].(string)

		// 检查是否已存在
		var existing int64
		h.DB.Model(&model.AlipayProduct{}).
			Where("app_id = ? AND parent_id = ? AND writeoff_id = ? AND account_type = 6 AND deleted_at IS NULL",
				smid, aliProduct.ID, writeoffID).
			Count(&existing)
		if existing > 0 {
			existCount++
			continue
		}

		// 创建新的授权商户
		merchantName, _ := order["merchant_name"].(string)
		if merchantName == "" {
			merchantName = fmt.Sprintf("%s直付通授权商户%d", smid, rand.Intn(900)+100)
		}
		parentID := aliProduct.ID
		newProduct := model.AlipayProduct{
			Name:        merchantName,
			AppID:       smid,
			WriteoffID:  &writeoffID,
			AccountType: 6,
			ParentID:    &parentID,
			Status:      true,
			CanPay:      true,
		}
		h.DB.Create(&newProduct)
		count++
	}

	if count == 0 {
		if existCount > 0 {
			response.DetailResponse(c, nil, fmt.Sprintf("已存在%d个授权的商户,请前往原生管理-支付宝菜单查看", existCount))
			return
		}
		response.ErrorResponse(c, "未查询到成功授权的商户")
		return
	}

	msg := fmt.Sprintf("成功添加%d个授权的商户", count)
	if existCount > 0 {
		msg += fmt.Sprintf(",%d个已存在", existCount)
	}
	msg += ",请前往原生管理-支付宝菜单查看"
	response.DetailResponse(c, nil, msg)
}

// IndirectRequest 提交进件申请（POST /api/alipay/sub/request/indirect/:indirect_id/request/）
func (h *AlipaySubRequestHandler) IndirectRequest(c *gin.Context) {
	indirectID, _ := strconv.Atoi(c.Param("indirect_id"))
	if indirectID == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}

	tenantID, err := h.getUserFilter(c)
	if err != nil {
		response.ErrorResponse(c, err.Error())
		return
	}

	// 查找直付通账号
	productQuery := h.DB.Where("id = ? AND account_type = 5 AND status = ? AND deleted_at IS NULL", indirectID, true)
	if tenantID > 0 {
		productQuery = productQuery.Joins("JOIN "+model.TablePrefix+"system_writeoff w ON w.id = "+model.TablePrefix+"alipay_product.writeoff_id").
			Where("w.parent_id = ?", tenantID)
	}
	var aliProduct model.AlipayProduct
	if err := productQuery.First(&aliProduct).Error; err != nil {
		response.ErrorResponse(c, "找不到直付通账号")
		return
	}

	var req struct {
		ExternalID string `json:"external_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var subProduct model.AlipaySubProduct
	if err := h.DB.Where("external_id = ?", req.ExternalID).First(&subProduct).Error; err != nil {
		response.ErrorResponse(c, "进件不存在")
		return
	}

	if subProduct.Smid != nil {
		response.ErrorResponse(c, "该商户已经成功进件")
		return
	}

	// 构建请求数据（排除不需要的字段）
	reqData := make(map[string]interface{})
	if subProduct.Name != nil && *subProduct.Name != "" {
		reqData["name"] = *subProduct.Name
	}
	if subProduct.AliasName != nil && *subProduct.AliasName != "" {
		reqData["alias_name"] = *subProduct.AliasName
	}
	if subProduct.MerchantType != nil && *subProduct.MerchantType != "" {
		reqData["merchant_type"] = *subProduct.MerchantType
	}
	if subProduct.Mcc != nil && *subProduct.Mcc != "" {
		reqData["mcc"] = *subProduct.Mcc
	}
	if subProduct.CertNo != nil && *subProduct.CertNo != "" {
		reqData["cert_no"] = *subProduct.CertNo
	}
	if subProduct.CertType != nil && *subProduct.CertType != "" {
		reqData["cert_type"] = *subProduct.CertType
	}
	if subProduct.CertImage != nil && *subProduct.CertImage != "" {
		reqData["cert_image"] = *subProduct.CertImage
	}
	if subProduct.CertImageBack != nil && *subProduct.CertImageBack != "" {
		reqData["cert_image_back"] = *subProduct.CertImageBack
	}
	if subProduct.LegalName != nil && *subProduct.LegalName != "" {
		reqData["legal_name"] = *subProduct.LegalName
	}
	if subProduct.LegalCertNo != nil && *subProduct.LegalCertNo != "" {
		reqData["legal_cert_no"] = *subProduct.LegalCertNo
	}
	if subProduct.AlipayLogonID != nil && *subProduct.AlipayLogonID != "" {
		reqData["alipay_logon_id"] = *subProduct.AlipayLogonID
	}
	if subProduct.BindingAlipayLogonID != nil && *subProduct.BindingAlipayLogonID != "" {
		reqData["binding_alipay_logon_id"] = *subProduct.BindingAlipayLogonID
	}
	if subProduct.LegalCertBackImage != nil && *subProduct.LegalCertBackImage != "" {
		reqData["legal_cert_back_image"] = *subProduct.LegalCertBackImage
	}
	if subProduct.LegalCertFrontImage != nil && *subProduct.LegalCertFrontImage != "" {
		reqData["legal_cert_front_image"] = *subProduct.LegalCertFrontImage
	}
	if subProduct.LicenseAuthLetterImage != nil && *subProduct.LicenseAuthLetterImage != "" {
		reqData["license_auth_letter_image"] = *subProduct.LicenseAuthLetterImage
	}
	if subProduct.ServicePhone != nil && *subProduct.ServicePhone != "" {
		reqData["service_phone"] = *subProduct.ServicePhone
	}
	if subProduct.SignTimeWithIsv != nil && *subProduct.SignTimeWithIsv != "" {
		reqData["sign_time_with_isv"] = *subProduct.SignTimeWithIsv
	}
	if subProduct.CertName != nil && *subProduct.CertName != "" {
		reqData["cert_name"] = *subProduct.CertName
	}
	if subProduct.LegalCertType != nil && *subProduct.LegalCertType != "" {
		reqData["legal_cert_type"] = *subProduct.LegalCertType
	}
	if subProduct.MerchantNature != nil && *subProduct.MerchantNature != "" {
		reqData["merchant_nature"] = *subProduct.MerchantNature
	}
	reqData["external_id"] = subProduct.ExternalID

	// JSON 字段（JSONSlice/JSONMap 已是 Go 原生类型，直接赋值）
	if len(subProduct.ContactInfos) > 0 {
		reqData["contact_infos"] = subProduct.ContactInfos
	}
	if len(subProduct.BizCards) > 0 {
		reqData["biz_cards"] = subProduct.BizCards
	}
	if len(subProduct.Service) > 0 {
		reqData["service"] = subProduct.Service
	}
	if len(subProduct.DefaultSettleRule) > 0 {
		reqData["default_settle_rule"] = subProduct.DefaultSettleRule
	}
	if len(subProduct.BusinessAddress) > 0 {
		reqData["business_address"] = subProduct.BusinessAddress
	}
	if len(subProduct.InvoiceInfo) > 0 {
		reqData["invoice_info"] = subProduct.InvoiceInfo
	}
	if len(subProduct.OutDoorImages) > 0 {
		reqData["out_door_images"] = subProduct.OutDoorImages
	}
	if len(subProduct.InDoorImages) > 0 {
		reqData["in_door_images"] = subProduct.InDoorImages
	}
	if len(subProduct.Sites) > 0 {
		reqData["sites"] = subProduct.Sites
	}
	if len(subProduct.Qualifications) > 0 {
		reqData["qualifications"] = subProduct.Qualifications
	}

	sdk, err := plugin.NewAlipaySDKFromProduct(h.DB, &aliProduct)
	if err != nil {
		response.ErrorResponse(c, "SDK初始化失败")
		return
	}

	res, err := sdk.IndirectZftCreate(reqData)
	if err != nil {
		response.ErrorResponse(c, "提交失败: "+err.Error())
		return
	}

	if code, _ := res["code"].(string); code != "10000" {
		errMsg := fmt.Sprintf("%v,%v,%v", res["msg"], res["sub_code"], res["sub_msg"])
		response.ErrorResponse(c, errMsg)
		return
	}

	orderID, _ := res["order_id"].(string)
	history := model.AlipaySubProductRequestHistory{
		OrderID:       orderID,
		SubMerchantID: subProduct.ExternalID,
	}
	h.DB.Create(&history)

	response.DetailResponse(c, nil, "提交成功")
}

// GetIndirectNotify 直付通进件回调通知（GET /api/alipay/sub/request/indirect/notify/）
func (h *AlipaySubRequestHandler) GetIndirectNotify(c *gin.Context) {
	log.Printf("IndirectNotify params: %v", c.Request.URL.Query())

	data := make(map[string]string)
	for k, v := range c.Request.URL.Query() {
		if len(v) > 0 {
			data[k] = v[0]
		}
	}

	appID := data["app_id"]
	var product model.AlipayProduct
	if err := h.DB.Where("app_id = ? AND account_type = 5 AND status = ? AND deleted_at IS NULL", appID, true).
		First(&product).Error; err != nil {
		c.String(200, "fail")
		return
	}

	sdk, err := plugin.NewAlipaySDKFromProduct(h.DB, &product)
	if err != nil {
		log.Printf("SDK初始化失败: %v", err)
		c.String(200, "fail")
		return
	}

	if !sdk.VerifyNotify(data) {
		log.Printf("验签失败")
		c.String(200, "fail")
		return
	}

	// 解析 biz_content
	bizContentStr := data["biz_content"]
	var bizContent map[string]interface{}
	if err := json.Unmarshal([]byte(bizContentStr), &bizContent); err != nil {
		log.Printf("解析biz_content失败: %v", err)
		c.String(200, "fail")
		return
	}

	orderID, _ := bizContent["order_id"].(string)
	var reqHistory model.AlipaySubProductRequestHistory
	if err := h.DB.Where("order_id = ?", orderID).First(&reqHistory).Error; err != nil {
		log.Printf("无请求记录: %s", orderID)
		c.String(200, "fail")
		return
	}

	msgMethod := data["msg_method"]
	if msgMethod == "ant.merchant.expand.indirect.zft.passed" {
		reqHistory.Status = "99"
		if v, ok := bizContent["card_alias_no"].(string); ok {
			reqHistory.CardAliasNo = &v
		}
		if v, ok := bizContent["smid"].(string); ok {
			reqHistory.Smid = &v
		}
		if v, ok := bizContent["memo"].(string); ok {
			reqHistory.Reason = &v
		}
		h.DB.Save(&reqHistory)

		// 创建授权商户
		var subMerchant model.AlipaySubProduct
		if err := h.DB.Where("external_id = ?", reqHistory.SubMerchantID).First(&subMerchant).Error; err == nil {
			name := ""
			if subMerchant.Name != nil {
				name = *subMerchant.Name
			}

			// 检查名称是否重复
			var nameCount int64
			h.DB.Model(&model.AlipayProduct{}).Where("name = ? AND deleted_at IS NULL", name).Count(&nameCount)
			if nameCount > 0 {
				name = name + "1"
			}

			smid := ""
			if reqHistory.Smid != nil {
				smid = *reqHistory.Smid
			}

			newProduct := model.AlipayProduct{
				AccountType: 6,
				Name:        name,
				WriteoffID:  &subMerchant.WriteoffID,
				AppID:       smid,
				Status:      true,
				CanPay:      true,
			}
			h.DB.Create(&newProduct)
		}
	} else if msgMethod == "ant.merchant.expand.indirect.zft.rejected" {
		reqHistory.Status = "-1"
		if v, ok := bizContent["reason"].(string); ok {
			reqHistory.Reason = &v
		}
		h.DB.Save(&reqHistory)
	}

	c.String(200, "success")
}

// IndirectRemote 生成远程进件授权（POST /api/alipay/sub/request/indirect/:indirect_id/remote/）
func (h *AlipaySubRequestHandler) IndirectRemote(c *gin.Context) {
	indirectID, _ := strconv.Atoi(c.Param("indirect_id"))
	if indirectID == 0 {
		response.ErrorResponse(c, "参数错误")
		return
	}

	user, exists := middleware.GetCurrentUser(c)
	if !exists {
		response.ErrorResponse(c, "未登录")
		return
	}

	var tenantID uint
	var writeoffID uint

	if user.Role.Key == model.RoleKeyWriteoff {
		var writeoff model.WriteOff
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&writeoff).Error; err != nil {
			response.ErrorResponse(c, "核销不存在")
			return
		}
		tenantID = writeoff.ParentID
		writeoffID = writeoff.ID
	} else if user.Role.Key == model.RoleKeyTenant {
		var tenant model.Tenant
		if err := h.DB.Where("system_user_id = ?", user.ID).First(&tenant).Error; err != nil {
			response.ErrorResponse(c, "租户不存在")
			return
		}
		tenantID = tenant.ID

		var req struct {
			Writeoff uint `json:"writeoff"`
		}
		if err := c.ShouldBindJSON(&req); err == nil {
			writeoffID = req.Writeoff
		}
	} else if user.Role.Key == model.RoleKeyAdmin || user.IsSuperuser {
		var req struct {
			Tenant   uint `json:"tenant"`
			Writeoff uint `json:"writeoff"`
		}
		if err := c.ShouldBindJSON(&req); err == nil {
			tenantID = req.Tenant
			writeoffID = req.Writeoff
		}
	} else {
		response.ErrorResponse(c, "没有权限")
		return
	}

	// 生成 auth token
	auth := fmt.Sprintf("%d", time.Now().UnixMilli())
	auth = auth[:5] + fmt.Sprintf("%d", writeoffID) + auth[5:]

	ctx := c.Request.Context()
	h.RDB.Set(ctx, fmt.Sprintf("sub_ten_%s", auth), tenantID, 24*time.Hour)
	h.RDB.Set(ctx, fmt.Sprintf("sub_%s", auth), indirectID, 24*time.Hour)
	h.RDB.Set(ctx, fmt.Sprintf("sub_wri_%s", auth), writeoffID, 24*time.Hour)

	response.DetailResponse(c, map[string]interface{}{
		"sub-auth": auth,
	}, "获取成功")
}

// Retrieve 获取进件详情（GET /api/alipay/sub/request/:external_id/）
func (h *AlipaySubRequestHandler) Retrieve(c *gin.Context) {
	externalID := c.Param("external_id")
	if externalID == "" {
		response.ErrorResponse(c, "参数错误")
		return
	}

	var subProduct model.AlipaySubProduct
	if err := h.DB.Where("external_id = ?", externalID).First(&subProduct).Error; err != nil {
		response.ErrorResponse(c, "不存在")
		return
	}

	response.DetailResponse(c, subProduct, "")
}
