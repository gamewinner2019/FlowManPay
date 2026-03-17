# Testing FlowManPay Go Backend

## Prerequisites
- Go 1.22+
- MySQL 5.7+ running on localhost:3306
- Redis running on localhost:6379
- Database `dvadmin` must exist

## Quick Start
```bash
# Start services
sudo service mysql start
sudo service redis-server start

# Create database if needed
mysql -u root -e "CREATE DATABASE IF NOT EXISTS dvadmin CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;"

# Build and run
cd /home/ubuntu/repos/FlowManPay
go build -o /tmp/flowmanpay cmd/server/main.go
/tmp/flowmanpay
```

Server starts on port 8000. GORM AutoMigrate creates all tables automatically.

## Admin User Setup
The admin user may need to be manually inserted on first run. Use the default password from `config.yaml` (`system.default_password`), hashed as `bcrypt(MD5(plaintext))`.

```sql
mysql -u root dvadmin -e "
INSERT INTO dvadmin_system_role (id, name, \`key\`, sort, status, admin, data_range, create_datetime, update_datetime)
VALUES (1, '管理员', 'admin', 1, 1, 1, 0, NOW(), NOW())
ON DUPLICATE KEY UPDATE id=id;

INSERT INTO dvadmin_system_users (username, password, name, is_active, status, role_id, create_datetime, update_datetime)
VALUES ('admin', '<BCRYPT_HASH_OF_MD5_DEFAULT_PASSWORD>', '管理员', 1, 1, 1, NOW(), NOW())
ON DUPLICATE KEY UPDATE id=id;
"
```

Generate the bcrypt hash with:
```bash
echo -n '<default_password>' | md5sum | awk '{print $1}' | htpasswd -niBC 10 '' | tr -d ':'
```

## SystemConfig Seeding
For testing `/api/init/settings/` and captcha features, seed the config table:
```sql
mysql -u root dvadmin -e "
INSERT INTO dvadmin_system_config (title, \`key\`, value, sort, status, form_item_type, parent_id, placeholder, create_datetime, update_datetime)
VALUES
('基本配置', 'base', NULL, 1, 1, 0, NULL, '', NOW(), NOW()),
('登录配置', 'login', NULL, 2, 1, 0, NULL, '', NOW(), NOW());

SET @base_id = (SELECT id FROM dvadmin_system_config WHERE \`key\` = 'base' AND parent_id IS NULL LIMIT 1);
SET @login_id = (SELECT id FROM dvadmin_system_config WHERE \`key\` = 'login' AND parent_id IS NULL LIMIT 1);

INSERT INTO dvadmin_system_config (title, \`key\`, value, sort, status, form_item_type, parent_id, placeholder, create_datetime, update_datetime)
VALUES
('验证码', 'captcha_state', '\"true\"', 1, 1, 0, @base_id, '', NOW(), NOW()),
('单点登录', 'single_login', '\"false\"', 2, 1, 0, @base_id, '', NOW(), NOW()),
('系统标题', 'system_title', '\"FlowManPay\"', 3, 1, 0, @base_id, '', NOW(), NOW()),
('登录页标题', 'title', '\"支付管理系统\"', 1, 1, 0, @login_id, '', NOW(), NOW()),
('登录页描述', 'desc', '\"欢迎使用\"', 2, 1, 0, @login_id, '', NOW(), NOW());
"
```

## Key Config Toggles (config.yaml)
- `system.login_no_captcha_auth: true` — enables `POST /api/login/` (no-captcha login). Default is false.
- `system.captcha_state: true` — enables captcha requirement on `/api/token/` login.

## Core API Testing Patterns

### Authentication Flow
```bash
# Get captcha
curl -s http://localhost:8000/api/captcha/

# Get captcha code from Redis
redis-cli GET "captcha:{key_from_captcha_response}"

# Login with captcha
curl -X POST http://localhost:8000/api/token/ \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123456","captchaKey":"{key}","captcha":"{code}"}'

# Login without captcha (requires config toggle)
curl -X POST http://localhost:8000/api/login/ \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123456"}'
```

### Public Endpoints (no auth)
- `GET /api/captcha/` — captcha image
- `GET /api/init/settings/` — system config (supports `?key=base` filtering)
- `POST /api/login/` — no-captcha login (if enabled)
- `POST /api/token/` — standard login
- `POST /api/token/refresh/` — refresh token

### Authenticated Endpoints
Use `Authorization: Bearer {access_token}` header for all authenticated endpoints.

## Important Testing Notes

### Captcha Behavior
- When `base.captcha_state` is set to `"true"` in SystemConfig table, `/api/token/` REQUIRES captcha.
- If no `base.captcha_state` row exists in SystemConfig, captcha defaults to REQUIRED.
- Captcha codes are stored in Redis with key `captcha:{id}` and 5-minute TTL.
- After successful login, the captcha code is deleted from Redis (one-time use).

### Config Reload
The Go binary reads `config.yaml` at startup only (via `sync.Once`). To apply config changes, you must rebuild and restart the server.

### Password Format
Passwords are hashed as `bcrypt(MD5(plaintext))`. The login flow tries MD5 first, then falls back to direct bcrypt comparison.

## Devin Secrets Needed
No external secrets needed for local testing. All services run on localhost with default credentials from config.yaml.
