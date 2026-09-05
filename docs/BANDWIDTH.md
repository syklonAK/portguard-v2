# محدودیت پهنای باند Per-UUID (PasarGuard)

> راهنمای فنی صادقانه — حتماً بخش «محدودیت‌های واقعی» را بخوانید.

## معماری

```
PasarGuard Panel (منبع کاربران: username/UUID)
        │  sync (interval قابل تنظیم)
        ▼
PortGuard Master (پنل)
  • rate_profiles (Basic/Premium/…)
  • rate_limit_policies (uuid + node → bps)
  • نگاشت UUID → source IP
        │  push plan (token-دار)
        ▼
PortGuard Agent روی نود
  • plan → tc (HTB + u32 filters)
  • state file + reconcile
        ▼
Linux Traffic Control (اعمال واقعی محدودیت)
```

## چرا این طراحی؟ (محدودیت بنیادی UUID)

**Linux tc در لایه بسته‌ها کار می‌کند؛ UUID در لایه پروتکل Xray است.**
هیچ ابزار tc/eBPF/nftables بدون پشتیبانی Xray نمی‌تواند UUID را از بسته‌ها بخواند.

زنجیره قابل‌اتکا:

```
UUID → (دیتابیس ما) → source IP عمومی کاربر → tc → سقف پهنای باند
```

پاسارگارد خودش نگاشت UUID→IP را دارد (از Xray gRPC API روی نود)، ولی آن API
داخلی پنل↔نود است و از بیرون قابل فراخوانی نیست. پس این سیستم سه منبع
برای IP کاربر دارد:

1. **API پنل پاسارگارد** — اگر admin مسیر users را روی پاسارگرد reverse-proxy
   کند، ما `last_ip` را از آن sync می‌کنیم (فیلد در جدول `pasarguard_users`).
2. **ورودی دستی** — در UI قابل ست شدن است (fallback همیشه‌کار).
3. **Sync دوره‌ای** — هر round دوباره بروزرسانی می‌شود.

## نحوه استفاده

1. **Settings → Bandwidth / PasarGuard**: URL پنل پاسارگارد + توکن admin API
   آن + فعال‌کردن rate limiting + interval.
2. **Bandwidth → Sync users**: کاربران با UUID وارد می‌شوند (idempotent).
3. **Bandwidth → Profiles**: پروفایل‌ها را بسازید (مثلاً Basic 5/2 Mbps،
   Premium 100/50 Mbps، مقدار 0 = unlimited).
4. روی هر کاربر **Set limit**: نود + پروفایل (یا Custom با مقادیر دلخواه).
5. **Push plans** (یا صبر برای sync خودکار): plan به agent نود فرستاده می‌شود
   و `tc` اعمال می‌گردد. وضعیت هر policy در UI (synced/failed/pending) ثبت
   و در Audit Log می‌ماند.

## اعمال tc — جزئیات فنی

- هر کاربرِ محدود = یک HTB class با دو filter u32 (match بر src برای
  آپلود، dst برای دانلود از دید نود).
- **Incremental**: تغییر یک کاربر فقط همان class را تغییر میدهد — کل
  qdisc از نو ساخته نمیشود (عامل حملات و پرش ترافیک).
- **Idempotent**: push تکراری با plan یکسان = no-op (state file مقایسه میشود).
- **Recovery**: agent بعد از restart از state file (`/var/lib/portguard/
  ratelimit-state.json`) وضعیت را میخواند و master در sync بعدی reconcile میکند.
- **Unlimited**: کاربر unlimited هیچ rule ای ندارد؛ تبدیل به unlimited =
  حذف class/filters او بدون دست زدن به بقیه.
- همه فراخوانی‌ها argv ساختاریافته (`exec.Command("tc", ...)`) — هیچ shell
  string ای از ورودی کاربر ساخته نمیشود.

## محدودیت‌های واقعی (مهم)

| مورد | توضیح |
|---|---|
| **IP مشترک** | چند کاربر پشت یک NAT/IPv4 عمومی = سقف **مشترک**. tc نمیتواند بینشان تفکیک کند. این محدودیت معماری است، نه باگ. |
| **تغییر IP** | کاربر موبایل/CGNAT با هر جابجایی IP باید نگاشت تازه شود؛ تا sync بعدی، سقف قبلی روی IP قبلی میماند و IP جدید موقتاً unlimited است. |
| **Xray mux** | اگر mux فعال باشد جریان چند کاربر در یک اتصال ادغام میشود؛ پاسارگارد معمولاً mux را خاموش نگه میدارد — در کانفیگ خودتان چک کنید. |
| **نیاز به root** | اعمال tc نیاز root دارد؛ agent خودش با root اجرا میشود (systemd unit). |
| **IPv6** | فیلترهای u32 بر IPv6 در نسخه فعلی پشتیبانی نمیشوند (فقط IPv4). |
| **افزایش IP در حالت گسترش** | بیش از ~۶۵هزار کاربر محدود در یک نود میتواند به hash classid بربخورد؛ در عمل نادیده گرفتنی است. |

## امنیت

- همه endpoint های agent با `Authorization: Bearer <node_token>` محافظت
  میشوند؛ UUID/مقادیر از کلاینت ناشناس هرگز قبول نمیشوند.
- توکن پاسارگارد فقط ذخیره و استفاده میشود؛ در هیچ API response ای
  برگردانده نمیشود (فقط `token_set` boolean).
- UUID ها قبل از هر عملیات validate میشوند (فرمت 8-4-4-4-12 hex).
- مقادیر bandwidth: عدد صحیح bps، بدون منفی/overflow (سقف 1 Tbps).
- Audit: هر تغییر policy (uuid, node, مقادیر, کاربر, نتیجه) ثبت میشود.

## API (خلاصه)

```
GET    /api/rate-limits/status          — خلاصه (کاربران/limited/failed/last_sync)
POST   /api/rate-limits/sync            — sync از پاسارگارد
GET    /api/rate-limits/profiles        — لیست/CRUD پروفایل‌ها
POST   /api/rate-limits/policies        — upsert policy (uuid+node)
DELETE /api/rate-limits/policies/{uuid}?node_id=N
POST   /api/rate-limits/push             — push plan ها به همه نودها
GET    /api/pasarguard/users            — کاربران + وضعیت policy

# روی agent (token-دار):
POST   /api/node/ratelimit/apply         — اعمال plan
GET    /api/node/ratelimit/state         — وضعیت اعمال‌شده
POST   /api/node/ratelimit/clear         — حذف همه rules
```
