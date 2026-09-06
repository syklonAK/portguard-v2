# PortGuard + Ansible — نصب یکجا و مدیریت Fleet

نصب و مدیریت کل زیرساخت PortGuard (مستر + نودها) با یک فرمان.

## ساختار

```
ansible/
├── ansible.cfg
├── inventory/hosts.ini        # مستر + نودها + نقش‌ها
├── site.yml                    # نصب/آپگرید همه‌چیز
├── service.yml                 # مدیریت سرویس‌ها از طریق panel API
└── roles/
    ├── portguard_panel/        # نصب مستر (packages + Go + build + systemd + ufw)
    ├── portguard_node/         # دیپلوی agent از مستر (توکن + باینری + سرویس)
    └── portguard_service/      # restart/reload/apply/probe از طریق API
```

## پیش‌نیاز

فقط روی **ماشین اپراتور** (لپ‌تاپ شما):

```bash
pip install ansible-core          # یا apt install ansible
```

روی سرورها هیچ چیز از قبل لازم نیست — فقط SSH root.

## ۱. آماده‌سازی inventory

`inventory/hosts.ini` را ویرایش کنید:

```ini
[portguard_master]
master-01 ansible_host=203.0.113.10

[portguard_nodes]
node-ir-01 ansible_host=198.51.100.11 portguard_node_role=iran
node-de-01 ansible_host=198.51.100.12 portguard_node_role=foreign
```

## ۲. نصب مستر

```bash
cd portguard/ansible
ansible-playbook site.yml --limit portguard_master
```

خروجی: پنل روی `http://MASTER:8080` بالا می‌آید — اولین بازدید حساب owner را میسازد.

## ۳. اضافه‌کردن نودها

اول در پنل: **Servers → Deploy new node** → توکن را کپی کنید. سپس:

```bash
ansible-playbook site.yml --limit portguard_nodes \
  -e portguard_agent_token=THE_TOKEN
```

هر نود باینری را از خود مستر دانلود میکند (نسخه همیشه همسان)، سرویس `portguard-agent` را میسازد، ufw را فقط برای IP مستر باز میکند و smoke-test میگیرد. در پنل: **Servers → Add server** با همان توکن.

## ۴. مدیریت سرویس‌ها (از طریق API — با audit و RBAC)

```bash
# ری‌استارت nginx (کانفیگ تولیدشده PortGuard)
ansible-playbook service.yml -e portguard_service_target=nginx \
  -e portguard_service_action=restart -e portguard_api_user=admin -e portguard_api_pass=PASS

# اعمال کامل کانفیگ (render → validate → backup → apply → rollback خودکار)
ansible-playbook service.yml -e portguard_service_target=apply \
  -e portguard_api_user=admin -e portguard_api_pass=PASS

# ری‌استارت همه agent های fleet
ansible-playbook service.yml -e portguard_service_target=agents \
  -e portguard_service_action=restart -e portguard_api_user=admin -e portguard_api_pass=PASS

# نمای وضعیت fleet
ansible-playbook service.yml -e portguard_service_target=probe \
  -e portguard_api_user=admin -e portguard_api_pass=PASS
```

## آپگرید

```bash
ansible-playbook site.yml --limit portguard_master   # git pull + rebuild + restart
ansible-playbook site.yml --limit portguard_nodes -e portguard_agent_token=...  # باینری تازه از مستر
```

## نکات

- **idempotent**: اجرای مجدد فقط تغییرات واقعی را اعمال میکند
- پنل هیچ‌وقت روی نود نصب نمیشود — فقط agent سبک
- `portguard_panel_port` و `portguard_agent_port` در inventory/group_vars قابل تغییرند
- TLS فعال‌سازی: `-e portguard_master_tls=true` (وقتی پنل پشت https است)
- دستورات سرویس از مسیر API میگذرند تا هر عملی **Audit Log** و **RBAC** بگیرد؛ systemd خام فقط برای خود پنل/agent استفاده میشود
