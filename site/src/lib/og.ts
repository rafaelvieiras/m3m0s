import { readFileSync } from "node:fs";
import { join } from "node:path";
import satori from "satori";
import { Resvg } from "@resvg/resvg-js";

const root = process.cwd();
const fontData = Object.fromEntries(
  [400, 600, 700].map((w) => [w, readFileSync(join(root, `node_modules/@fontsource/inter/files/inter-latin-${w}-normal.woff`))]),
);
const font = (weight: number) => fontData[weight];
const iconSvg = readFileSync(join(root, "src/lib/icon-dark.svg"), "utf8");
const iconUri = `data:image/svg+xml;base64,${Buffer.from(iconSvg).toString("base64")}`;

type Node = { type: string; props: Record<string, unknown> };
const h = (type: string, style: Record<string, unknown>, children?: unknown, extra: Record<string, unknown> = {}): Node => ({
  type,
  props: { style: { display: "flex", ...style }, children, ...extra },
});

export interface OgInput {
  title: string;
  description: string;
  kicker?: string;
}

export async function renderOg({ title, description, kicker }: OgInput): Promise<Buffer> {
  const long = title.length > 34;
  const tree = h(
    "div",
    {
      width: 1200, height: 630, boxSizing: "border-box", flexDirection: "column", justifyContent: "space-between",
      padding: "64px 72px", color: "#ececef", fontFamily: "Inter",
      background: "linear-gradient(135deg, #0e1013 0%, #14181d 55%, #10302d 130%)",
    },
    [
      h("div", { alignItems: "center", gap: 20 }, [
        h("img", { width: 96, height: 59 }, undefined, { src: iconUri, width: 96, height: 59 }),
        h("div", { fontSize: 44, fontWeight: 700, letterSpacing: -1 }, "m3m0s"),
        kicker ? h("div", { marginLeft: 12, fontSize: 24, fontWeight: 600, color: "#5eead4", background: "#12302d", padding: "6px 16px", borderRadius: 999 }, kicker) : h("div", {}, ""),
      ]),
      h("div", { flexDirection: "column", gap: 20 }, [
        h("div", { fontSize: long ? 64 : 76, fontWeight: 700, letterSpacing: -2, lineHeight: 1.05 }, title),
        h("div", { fontSize: 30, fontWeight: 400, color: "#a0a3ad", lineHeight: 1.35, maxWidth: 980 }, description),
      ]),
      h("div", { width: "100%", justifyContent: "space-between", fontSize: 24, color: "#a0a3ad" }, [
        h("div", {}, "m3m0s.rafaelvieiras.com"),
        h("div", { color: "#5eead4", fontWeight: 600 }, "Long-term memory for LLM agents"),
      ]),
    ],
  );
  const svg = await satori(tree as never, {
    width: 1200,
    height: 630,
    fonts: [
      { name: "Inter", data: font(400), weight: 400, style: "normal" },
      { name: "Inter", data: font(600), weight: 600, style: "normal" },
      { name: "Inter", data: font(700), weight: 700, style: "normal" },
    ],
  });
  return Buffer.from(new Resvg(svg, { fitTo: { mode: "width", value: 1200 } }).render().asPng());
}
