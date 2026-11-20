#!/usr/bin/env python3
"""Exporta las tarjetas de Novedades a GIF para las notas de release.

Uso:
    python3 scripts/release_media.py --version 2.54.0 [--url http://localhost:4180]

Necesita el Playwright del sistema (uv tool) y ffmpeg. El proceso completo
está en docs/releases/README.md: estas imágenes se generan desde la app, no se
graban a mano, así que siguen al tema y al idioma del día que se cortan.

El script:
  1. abre la app con sesión de demo (no hace falta backend con datos),
  2. intercepta /api/health para que la página crea que corre la versión pedida,
  3. espera a que carguen las tarjetas de /whatsnew,
  4. captura cada demo (`[data-demo-id]`) a 10 fps durante unos segundos,
  5. monta un GIF con ffmpeg y lo deja en docs/releases/<version>/<id>.gif.
"""
import argparse
import os
import shutil
import subprocess
import sys
import tempfile

PLAYWRIGHT_PY = os.path.expanduser("~/.local/share/uv/tools/playwright/bin/python")
CHROMIUM = os.path.expanduser(
    "~/.cache/ms-playwright/chromium_headless_shell-1255/chrome-headless-shell-linux64/chrome-headless-shell"
)
CARDS = ["icon-picker", "dns-activity", "presence", "languages"]
FPS = 10
SECONDS = 4
WIDTH = 660


def run(version: str, base_url: str) -> int:
    if not os.path.exists(PLAYWRIGHT_PY):
        print(f"falta el Playwright de uv en {PLAYWRIGHT_PY}", file=sys.stderr)
        return 1
    root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    out_dir = os.path.join(root, "docs", "releases", version)
    os.makedirs(out_dir, exist_ok=True)

    script = r"""
import asyncio, os, sys
from playwright.async_api import async_playwright

BASE = sys.argv[1]
VERSION = sys.argv[2]
OUT = sys.argv[3]
CARDS = sys.argv[4].split(",")
SHELL = sys.argv[5]
FPS = int(sys.argv[6])
SECONDS = int(sys.argv[7])

async def main():
    async with async_playwright() as p:
        browser = await p.chromium.launch(executable_path=SHELL)
        ctx = await browser.new_context(viewport={"width": 1440, "height": 1000}, device_scale_factor=2)
        page = await ctx.new_page()
        await page.route("**/api/health", lambda r: r.fulfill(
            status=200, content_type="application/json",
            body='{"ok":true,"version":"%s","db":"ok","mode":"live","devicesTotal":0,"agentsConnected":0}' % VERSION))
        await page.goto(BASE + "/login")
        await page.evaluate("() => { sessionStorage.setItem('netpulse-demo','1'); localStorage.setItem('netpulse-theme-mode', JSON.stringify('dark')); }")
        await page.goto(BASE + "/whatsnew")
        await page.wait_for_timeout(1200)
        for card in CARDS:
            el = page.locator('[data-demo-id="%s"]' % card)
            if await el.count() == 0:
                print("sin tarjeta: " + card, file=sys.stderr)
                continue
            await el.scroll_into_view_if_needed()
            await page.wait_for_timeout(300)
            frames_dir = os.path.join(OUT, card + "-frames")
            os.makedirs(frames_dir, exist_ok=True)
            for i in range(FPS * SECONDS):
                await el.screenshot(path=os.path.join(frames_dir, "f%03d.png" % i))
                await page.wait_for_timeout(1000 // FPS)
            print(frames_dir)
        await browser.close()

asyncio.run(main())
"""
    frames_root = tempfile.mkdtemp(prefix="np-release-media-")
    try:
        proc = subprocess.run(
            [PLAYWRIGHT_PY, "-c", script, base_url, version, frames_root, ",".join(CARDS), CHROMIUM, str(FPS), str(SECONDS)],
            capture_output=True, text=True,
        )
        if proc.returncode != 0:
            print(proc.stdout, proc.stderr, file=sys.stderr)
            return proc.returncode
        made = 0
        for card in CARDS:
            frames_dir = os.path.join(frames_root, card + "-frames")
            if not os.path.isdir(frames_dir):
                continue
            gif = os.path.join(out_dir, card + ".gif")
            palette = os.path.join(frames_root, card + "-palette.png")
            subprocess.run([
                "ffmpeg", "-y", "-loglevel", "error", "-framerate", str(FPS),
                "-i", os.path.join(frames_dir, "f%03d.png"),
                "-vf", f"scale={WIDTH}:-1:flags=lanczos,palettegen=stats_mode=diff", palette,
            ], check=True)
            subprocess.run([
                "ffmpeg", "-y", "-loglevel", "error", "-framerate", str(FPS),
                "-i", os.path.join(frames_dir, "f%03d.png"),
                "-i", palette, "-lavfi", f"scale={WIDTH}:-1:flags=lanczos[x];[x][1:v]paletteuse=dither=bayer",
                "-loop", "0", gif,
            ], check=True)
            print(f"{gif}  {os.path.getsize(gif) // 1024} KiB")
            made += 1
        return 0 if made else 1
    finally:
        shutil.rmtree(frames_root, ignore_errors=True)


if __name__ == "__main__":
    ap = argparse.ArgumentParser()
    ap.add_argument("--version", required=True)
    ap.add_argument("--url", default="http://localhost:4180")
    args = ap.parse_args()
    sys.exit(run(args.version, args.url))
