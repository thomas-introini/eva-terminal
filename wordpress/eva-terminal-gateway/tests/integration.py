#!/usr/bin/env python3
"""Native Woo + isolated Stripe SDK + deterministic test transport; no production credentials."""
import hashlib
import hmac
import json
import os
import time
import urllib.error
import urllib.request
import uuid

BASE = os.environ.get("EVA_TEST_URL", "http://127.0.0.1:18081")
KEY = os.environ["EVA_BRIDGE_KEY"]


def request(path, body=None, token="", extra=None, expected=200):
    headers = {"Content-Type": "application/json", "X-EVA-Bridge-Key": KEY}
    if token:
        headers["Cart-Token"] = token
    headers.update(extra or {})
    req = urllib.request.Request(BASE + "/wp-json/" + path, headers=headers,
                                 data=None if body is None else json.dumps(body).encode())
    try:
        response = urllib.request.urlopen(req, timeout=40)
    except urllib.error.HTTPError as error:
        response = error
    raw = response.read()
    assert (200 <= response.status < 300 if expected == 200 else response.status == expected), (path, response.status, raw.decode())
    return json.loads(raw), response.headers


def new_checkout(product, lose=False, expected_state="awaiting_payment", analytics="valid"):
    grinds = product.get("extensions", {}).get("eva_terminal", {}).get("grinds", [])
    cart, headers = request("wc/store/v1/cart")
    token = headers["Cart-Token"]
    cart, _ = request("wc/store/v1/cart/add-item", {"id": product["id"], "quantity": 1,
                      "extensions": {"eva_terminal": {"grind": "Espresso" if "Espresso" in grinds else ""}}}, token)
    address = {"first_name": "Ada", "last_name": "Lovelace", "email": "ada@example.com",
               "address_1": "Via Roma 1", "city": "Roma", "state": "RM", "postcode": "00100",
               "country": "IT", "phone": "061234567"}
    cart, _ = request("wc/store/v1/cart/update-customer", {"billing_address": address, "shipping_address": address}, token)
    cart, _ = request("wc/store/v1/cart/extensions", {"namespace": "eva_terminal", "data": {"prepare": True}}, token)
    if grinds:
        cart, _ = request("wc/store/v1/cart/apply-coupon", {"code": "COFFEE10"}, token)
        assert cart["coupons"], cart
    if cart["needs_shipping"]:
        assert cart["shipping_rates"], "No native shipping rates"
        for package in cart["shipping_rates"]:
            rate = package["shipping_rates"][0]
            cart, _ = request("wc/store/v1/cart/select-shipping-rate", {"package_id": package["package_id"], "rate_id": rate["rate_id"]}, token)
    quote = cart["extensions"]["eva_terminal"]["quote"]
    payload = {"attempt_id": uuid.uuid4().hex, "customer_ref": hashlib.sha256(uuid.uuid4().bytes).hexdigest(),
               "accepted_quote": quote, "checkout": {"billing_address": address, "shipping_address": address}}
    if analytics == "valid":
        payload["analytics"] = {"connection_id": str(uuid.uuid4()), "checkout_id": str(uuid.uuid4()),
                                "schema_version": 1, "environment": "staging", "collect": True}
    elif analytics == "malformed":
        payload["analytics"] = {"connection_id": "invalid", "base_url": "https://untrusted.example"}
    changed = json.loads(json.dumps(payload))
    changed["accepted_quote"]["total"] = str(int(quote["total"]) + 1)
    request("eva-terminal/v1/checkout", changed, token, expected=409)
    attempt, _ = request("eva-terminal/v1/checkout", payload, token,
                         {"X-EVA-Test-Lose-Session": "1"} if lose else None)
    if lose:
        held, _ = request("eva-test/v1/order/" + str(attempt["order_id"]))
        assert held["hold_until"] >= attempt["expires_at"] + 300, (held, attempt)
        attempt, _ = request("eva-terminal/v1/checkout", payload, token)
    assert attempt["payment_state"] == expected_state, attempt
    again, _ = request("eva-terminal/v1/checkout", payload, token)
    assert again["order_id"] == attempt["order_id"] and again["payment_url"] == attempt["payment_url"]
    changed = json.loads(json.dumps(payload))
    changed["checkout"]["customer_note"] = "Different request"
    request("eva-terminal/v1/checkout", changed, token, expected=409)
    request("eva-terminal/v1/attempts/" + payload["attempt_id"] + "?customer_ref=" + "0" * 64, expected=404)
    return payload, attempt


def webhook(session, kind="checkout.session.completed", event_id="evt_test_duplicate"):
    body = json.dumps({"id": event_id, "object": "event", "api_version": "2026-09-30.endive",
                       "type": kind, "livemode": False, "data": {"object": session}}).encode()
    timestamp = int(time.time())
    signature = hmac.new(b"whsec_eva_local_test", str(timestamp).encode() + b"." + body, hashlib.sha256).hexdigest()
    req = urllib.request.Request(BASE + "/wp-json/eva-terminal/v1/stripe/webhook", data=body,
                                 headers={"Content-Type": "application/json", "Stripe-Signature": f"t={timestamp},v1={signature}"})
    with urllib.request.urlopen(req, timeout=40) as response:
        assert response.status == 200


products, _ = request("wc/store/v1/products?per_page=100")
product = next(p for p in products if p["type"] == "simple" and p["is_in_stock"] and "Espresso" in p["extensions"]["eva_terminal"]["grinds"])
request("eva-terminal/v1/stripe/webhook", {}, expected=400)
payload, attempt = new_checkout(product, lose=True)
session_id = attempt["payment_url"].split("/")[-1]
session, _ = request("eva-test/v1/session/" + session_id, {"state": "paid"})
webhook(session)
webhook(session)  # duplicate
webhook(session, "checkout.session.expired", "evt_test_old_expiry")  # out of order: Stripe is paid
paid, _ = request("eva-terminal/v1/attempts/" + payload["attempt_id"] + "?customer_ref=" + payload["customer_ref"])
assert paid["payment_state"] == "paid", paid
cancelled, _ = request("eva-terminal/v1/attempts/" + payload["attempt_id"] + "/cancel", {"customer_ref": payload["customer_ref"]})
assert cancelled["payment_state"] == "paid", cancelled
payload2, attempt2 = new_checkout(product)
cancelled, _ = request("eva-terminal/v1/attempts/" + payload2["attempt_id"] + "/cancel", {"customer_ref": payload2["customer_ref"]})
assert cancelled["payment_state"] == "cancelled", cancelled
payload3, attempt3 = new_checkout(product)
session3, _ = request("eva-test/v1/session/" + attempt3["payment_url"].split("/")[-1], {"state": "expired"})
webhook(session3, "checkout.session.expired", "evt_test_expiry")
expired, _ = request("eva-terminal/v1/attempts/" + payload3["attempt_id"] + "?customer_ref=" + payload3["customer_ref"])
assert expired["payment_state"] == "expired", expired
print("Native Woo checkout: quote/owner checks, interrupted creation, idempotency, signed duplicate/reordered webhooks, payment/cancel race and expiry passed")

variable = next(p for p in products if p["type"] == "variable" and p["is_in_stock"])
variations, _ = request("wc/store/v1/products?type=variation&parent=" + str(variable["id"]) + "&per_page=100")
assert variations, "No native variations"
variant = next(v for v in variations if v["is_in_stock"])
variant["extensions"] = variable["extensions"]
vp, va = new_checkout(variant)
request("eva-terminal/v1/attempts/" + vp["attempt_id"] + "/cancel", {"customer_ref": vp["customer_ref"]})
zero = next(p for p in products if p["name"] == "EVA Free Test Sample")
zp, za = new_checkout(zero, expected_state="paid")
assert za["total"] == "0" and not za["payment_url"], za
new_checkout(zero, expected_state="paid", analytics="malformed")
new_checkout(zero, expected_state="paid", analytics="legacy")
# Payment winning cancellation must enqueue even without an SSH client or webhook.
cp, ca = new_checkout(product)
request("eva-test/v1/session/" + ca["payment_url"].split("/")[-1], {"state": "paid"})
won, _ = request("eva-terminal/v1/attempts/" + cp["attempt_id"] + "/cancel", {"customer_ref": cp["customer_ref"]})
assert won["payment_state"] == "paid", won
print("Native variations, coupons, shipping selection and zero-total checkout passed")
