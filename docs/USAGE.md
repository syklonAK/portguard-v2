# PortGuard — راهنمای کامل استفاده

> نسخه فارسی. برای انگلیسی: [README.en.md](../README.en.md)

این داکیومنت همه‌ی قابلیت‌های سرویس را مرحله‌به‌مرحله توضیح می‌دهد: نصب، پنل، مپینگ‌ها، مسیریابی پویا، سایت ضد DPI، تانل هدیوم، لاگ لایو اتصالات، آپدیتر و عیب‌یابی.

## فهرست

1. [نصب و حذف](#۱-نصب-و-حذف)
2. [اولین ورود](#۲-اولین-ورود)
3. [داشبورد و پورت‌ها](#۳-داشبورد-و-پورت‌ها)
4. [مپینگ‌ها (ریورس‌پروکسی)](#۴-مپینگ‌ها-ریورسپروکسی)
5. [مسیریابی پویا ws/httpupgrade/xhttp](#۵-مسیریابی-پویا-wshttpupgradexhttp)
6. [سایت فیک ضد DPI](#۶-سایت-فیک-ضد-dpi)
7. [تانل Hedioum](#۷-تانل-hedioum)
8. [لاگ لایو اتصالات](#۸-لاگ-لایو-اتصالات)
9. [گواهی‌های SSL](#۹-گواهیهای-ssl)
10. [سرویس‌ها، Runtime و بکاپ](#۱۰-سرویسها-runtime-و-بکاپ)
11. [پایش و عیب‌یابی](#۱۱-پایش-و-عیبیابی)
12. [آپدیتر خودکار](#۱۲-آپدیتر-خودکار)
13. [امنیت](#۱۳-امنیت)

---

## ۱. نصب و حذف

### نصب با یک خط (اوبونتو 20.04 / 22.04 / 24.04)

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/syklonAK/portguard-v2/main/deploy/remote-install.sh)"
```

پورت دلخواه:

```bash
sudo PORTGUARD_PORT=9000 bash -c "$(curl -fsSL https://raw.githubusercontent.com/syklonAK/portguard-v2/main/deploy/remote-install.sh)"
```

نصب‌کننده خودکار: nginx + haproxy + `libnginx-mod-stream` را نصب و خنثی می‌کند، در نبود Go آن را نصب می‌کند، باینری را می‌سازد (فرانت از قبل build شده — نیازی به Node نیست)، ufw را در صورت فعال بودن باز می‌کند و سرویس systemd را فعال می‌کند.

### به‌روزرسانی بدون نصب مجدد

از پنل: **Settings → Panel updater → Check & update now**
یا از سرور: `sudo bash /opt/portguard/deploy/update.sh`

### حذف

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/syklonAK/portguard-v2/main/deploy/uninstall.sh)"
# کامل با دیتا:
sudo bash -c "$(curl .../uninstall.sh)" -- --purge-data --yes
# خیلی کامل (حتی nginx/haproxy):
sudo bash -c "$(curl .../uninstall.sh)" -- --purge-data --purge-packages --yes
```

## ۲. اولین ورود

اولین بازدید از `http://<server-ip>:8080` ویزارد ساخت اکانت ادمین را نشان می‌دهد (نام‌کاربری + پسورد ۸ کاراکتری). دیتابیس حاوی ادمین باشد، به‌جای ویزارد فرم لاگین می‌آید. فراموشی پسورد = حذف `/var/lib/portguard/portguard.db` (ریست کامل).

## ۳. داشبورد و پورت‌ها

- **Dashboard**: CPU/RAM/دیسک، تعداد مپینگ‌ها، سلامت بک‌اند‌ها — لایو با SSE.
- **Ports**: اسکن کل پورت‌های TCP/UDP در حال شنود با پروسه و کاربر مالک. پورت‌های **unmanaged** قرمز نشان داده می‌شوند؛ دکمه **Map** مستقیم فرم ساخت مپینگ را با همان پورت پر می‌کند.

## ۴. مپینگ‌ها (ریورس‌پروکسی)

هر مپینگ = یک شنونده → یک یا چند بک‌اند، با انتخاب موتور:

| فیلد | توضیح |
|---|---|
| Engine | `nginx` یا `haproxy` — هر مپینگ مستقل |
| Protocol | http / https / tcp / udp (udp فقط nginx) |
| Listen IP:Port | جایی که کاربر وصل می‌شود |
| Server names | دامنه‌ها (فقط L7) |
| Targets | بک‌اندها با وزن و حالت backup |
| Balance | الگوریتم لودبالانس مخصوص هر موتور |
| Path prefix | فقط این مسیر پروکسی شود؛ بقیه ۴۰۴ یا سایت فیک |
| Access list | allow/deny بر اساس IP یا CIDR — اولین تطبیق برنده؛ با وجود allow، بقیه رد می‌شوند |
| WebSocket / HTTP/2 | برای ترافیک دائمی و TLS |
| Host header | هدری که به‌جای `$host` به بک‌اند فرستاده می‌شود (ریلی به نود خارجی) |
| Redirect to | 301 بدون بک‌اند |

سه رابط برای ساخت: **Visual** (فرم)، **Raw JSON** (ویرایش آزاد کل آبجکت با اعتبارسنجی زنده) و **Templates** (قالب‌های آماده).

> جریان امن Apply: رندر → اعتبارسنجی (`nginx -t` / `haproxy -c`) → بکاپ زمان‌دار → نوشتن اتمیک → reload بدون قطعی → **rollback خودکار** در صورت خطا. کانفیگ نامعتتبر هرگز به سرویس زنده نمی‌رسد.

## ۵. مسیریابی پویا ws/httpupgrade/xhttp

الگوی `/<prefix>/<port>`: پورت مقصد از خود path استخراج می‌شود — یک دامنه و یک پورت TLS همه‌ی اینباند‌های Xray را سرویس می‌کند.

مثال: mapping با `https:443` و سه path route:

- `ws` (WebSocket) → `/ws/10000` → `127.0.0.1:10000` — نیازمند هدر Upgrade
- `httpupgrade` → `/hu/10000` → همان رفتار با پورت دیگر
- `xhttp` → `/xhttp/10000` → بدون Upgrade، با `proxy_buffering off` برای استریم

هر route می‌تواند بازه پورت مجاز داشته باشد (مثل 10000–10003) که در nginx تبدیل به regex و در haproxy به ACL با آلترنیشن می‌شود. پیش‌نمایش زنده URLها پایین فرم نمایش داده می‌شود.

در پنل Xray برای هر inbound: `listen=127.0.0.1`، `security=none`، path = `/<prefix>/<پورت local>`.

## ۶. سایت فیک ضد DPI

در فرم مپینگ (موتور nginx، L7): بخش **Decoy site (anti-DPI)**:

- **Builtin** — صفحه فرود SaaS واقعی‌نما (سرویس مانیتورینگ) از `/var/lib/portguard/decoy/` سرو می‌شود.
- **Custom** — HTML خودتان.
- **Off** — مسیرهای ناشناخته 404 می‌شوند.

نتیجه: DPI و اسکنرها به‌جای 404 مشکوک، یک وب‌سایت عادی می‌بینند؛ ترافیک ws/hu/xhttp از زیر آن رد می‌شود.

## ۷. تانل Hedioum

صفحه **Tunnels** سمت ایرانِ توپولوژی است:

```
user → این سرور (entry) → xray dokodemo (bridge) → SOCKS5 hub → egress خارجی → نود
```

- **Environment**: نصب بودن hedioum/xray، وضعیت سرویس‌ها، SOCKS hub شناسایی‌شده، نقش سرور.
- **Relay** ها: هر رلی یک مقصد خارجی؛ دو مود:
  - `raw` — شنونده عمومی TCP، پاس‌ترو مستقیم؛ TLS/REALITY روی نود خارجی می‌ماند.
  - `tls` — TLS همین‌جا تمام می‌شود: یک mapping با `https` بسازید که به `127.0.0.1:<bridge_port>` پروکسی کند و Host header نود خارجی را بفرستد.
- **Apply bridge** — تولید `portguard-bridge.json`، تست با `xray run -test`، بکاپ، نصب اتمیک و restart با rollback خودکار.
- پورت SOCKS hub در **Settings → Tunnel** تنظیم می‌شود (پیش‌فرض 127.0.0.1:40001).

سمت خارجی (egress) همچنان با اسکریپت رسمی خودش تنظیم می‌شود؛ PortGuard فرآیند ایران را اتومات می‌کند.

## ۸. لاگ لایو اتصالات

صفحه **Connections** — نمای امنیتی لایو از `ss`:

- **هر اتصال ESTAB ورودی**: IP:پورت مبدأ، پورت مقصد، پروسه‌ی سرویس‌دهنده، مدت اتصال.
- دسته‌بندی: **managed** (پورتِ تحت مپینگ یا nginx/haproxy/xray)، **unmanaged** (باید بررسی شود!)، **panel session** (ادمین‌ها).
- **Top talkers**: پرسر‌وصداترین IPهای خارجی با مقصدهای پرتکرارشان.
- فیلتر (managed/unmanaged) و جستجو (IP/پورت/پروسه) + دکمه Pause/Resume.
- اسنپ‌شات هر ۵ ثانیه در DB ذخیره می‌شود؛ `first_seen` بین سیکل‌ها حفظ می‌شود تا مدت اتصال واقعی دیده شود.

نکته امنیتی: اتصال به پورتی که هیچ mapping نداره یعنی سرویسی بیرون از کنترل پنل در حال سرویس‌دهی است — بررسی، مپ یا فایروال کنید.

## ۹. گواهی‌های SSL

صفحه **SSL Certs**: آپلود جفت PEM یا ساخت self-signed چنددامنه‌ای. خودکار برای nginx به `.crt/.key` و برای haproxy به `.pem` ترکیبی تبدیل و در `/var/lib/portguard/certs` با مجوز 600 ذخیره می‌شود. دکمه اعتبارسنجی، انقضا و دامنه‌ها را چک می‌کند.

## ۱۰. سرویس‌ها، Runtime و بکاپ

- **Services**: start/stop/restart/status برای nginx و haproxy.
- **HAProxy Runtime**: `show info/stat` زنده از سوکت runtime؛ تغییر وضعیت سرورها (ready/drain/maint) بدون reload.
- **Backups**: بکاپ‌های زمان‌دار کانفیگ با diff رنگی و بازگردانی اعتبارسنجی‌شده با یک کلیک.
- **Diagnostics**: تست TCP / DNS / TLS / HTTP / اتصال به بک‌اند از خود پنل.
- **Import/Export**: اسنپ‌شات JSON همه‌ی مپینگ‌ها (بدون کلیدهای خصوصی).

## ۱۱. پایش و عیب‌یابی

- **Monitoring**: چک دوره‌ای TCP/HTTP هر بک‌اند با تأخیر و شمارش خطای متوالی (SSE).
- **Audit Log**: همه‌ی اقدامات (ورود، تغییر، apply، ...) با کاربر و نتیجه.

| مشکل | راه‌حل |
|---|---|
| پنل بالا نمی‌آید | `systemctl status portguard` و `journalctl -u portguard -n 50` |
| خطای Apply | پیام، خروجی واقعی `nginx -t`/`haproxy -c` است؛ مپینگ مربوطه را اصلاح کنید — rollback خودکار انجام شده |
| خطای bind در Apply | پورت در اختیار پروسه دیگری است (صفحه Ports)؛ rollback شده |
| Bridge فعال نمی‌شود | xray نصب نیست؟ `systemctl status portguard-tunnel-bridge` و `journalctl -u portguard-tunnel-bridge -n 30` |
| فراموشی پسورد ادمین | حذف DB → ریست کامل (مپینگ‌ها هم پاک می‌شوند) |

## ۱۲. آپدیتر خودکار

`deploy/update.sh`:

1. `git fetch` + fast-forward به `origin/main`
2. بیلد مجدد باینری (فرنت embed شده — Node لازم نیست)
3. سواپ اتمیک باینری با بکاپ
4. restart سرویس؛ در خطا rollback به باینری قبلی

از پنل: Settings → Panel updater. بعد از چند ثانیه پنل با بیلد جدید روی همان پورت بالا می‌آید.

## ۱۳. امنیت

- پنل root اجرا می‌شود (نوشتن `/etc/nginx` و `/etc/haproxy` و reload سرویس‌ها) — پورت پنل را با فایروال محدود کنید.
- سشن JWT (HS256)، bcrypt، محدودیت نرخ ورود per-IP، audit کامل.
- کلیدهای خصوصی با 0600 در `/var/lib/portguard/certs`.
- هر Apply یک بکاپ زمان‌دار در `/var/lib/portguard/backups` باقی می‌گذارد.
- توصیه: پورت 8080 را فقط به IPهای مطمئن باز کنید؛ صفحه Connections را برای دیدن چه کسی متصل است زیر نظر بگیرید.

---

ساختار پروژه و معماری در [README.md](../README.md) توضیح داده شده است.
