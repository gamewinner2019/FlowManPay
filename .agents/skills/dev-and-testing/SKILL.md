# FlowManPay Go Backend - Development & Testing

## Build & Lint
```bash
cd /home/ubuntu/repos/FlowManPay
go build ./...
go vet ./...
```

## Prerequisites
- MySQL 8.0+ running on localhost:3306
- Redis running on localhost:6379
- Go 1.22+

## Database Setup
```sql
CREATE DATABASE dvadmin CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;
```
Tables are auto-created via GORM AutoMigrate on server startup.

## Configuration
Config file: `config.yaml` in project root. Key settings:
- `database.*` - MySQL connection (host, port, user, password, dbname)
- `redis.*` - Redis connection
- `server.port` - Default 8000
- `jwt.secret` - JWT signing key
- Table prefix: `dvadmin_`
- Timezone: Asia/Shanghai

## Start Server
```bash
go run cmd/server/main.go
```
Server listens on `:8000`. Logs to stdout.

## Admin User Setup
Password hashing uses MD5(plaintext) then bcrypt. To create admin user, insert into `dvadmin_system_role` (key=admin) and `dvadmin_system_users` (username=admin) with a bcrypt hash of the MD5 of the desired password.

Admin credentials: username=`admin`, password stored via secret management.

## API Testing Patterns

### Public endpoints (no auth):
- `GET /api/captcha/` - Returns `{"code":2000, "data":{"key":"...", "image_base":"data:image/png;base64,..."}}`
- `POST /api/token/` - Login with `{"username","password","captchaKey"(optional),"captcha"(optional)}`

### Protected endpoints:
Use `Authorization: JWT <access_token>` header.

### Response format:
- Success: `{"code": 2000, "data": {...}, "msg": "...", "success": true}`
- Error: `{"code": 4000, "data": null, "msg": "...", "success": false}`
- Auth error: code 4001

### Captcha flow:
1. `GET /api/captcha/` to get key + image
2. Captcha code stored in Redis as `captcha:{key}` with 5min TTL
3. Send `captchaKey` + `captcha` in login request
4. Captcha is consumed (deleted) after successful validation

### Token blacklist (logout):
- `POST /api/logout/` adds token to Redis key `blacklist:{token}`
- Subsequent requests with blacklisted token return code 4001

## Module Path
`github.com/gamewinner2019/FlowManPay`

## Key Architecture Notes
- Internal packages under `internal/` - test files must be inside the module, not in /tmp
- Optimistic locking pattern: `Where("version = ?", v).Updates(map[string]interface{}{"field": val, "version": v+1})`
- Order IDs: `CreateOrderID()` = 21 digits, `CreateOrderNo()` = G + 24 digits, `CreateRechargeNo()` = R + 24 digits
