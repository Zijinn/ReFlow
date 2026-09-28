import { mkdir } from "node:fs/promises"
import { resolve } from "node:path"
import sharp from "sharp"

const sourcePath = resolve(import.meta.dirname, "../assets/brand/reflow-product-icon.png")
const outputDir = resolve(import.meta.dirname, "../public/icons")
const nativeOutputDir = resolve(import.meta.dirname, "../../build/icons")
await mkdir(outputDir, { recursive: true })
await mkdir(nativeOutputDir, { recursive: true })

const metadata = await sharp(sourcePath).metadata()
if (metadata.width !== metadata.height || (metadata.width ?? 0) < 1024) {
  throw new Error("ReFlow icon source must be a square image at least 1024px wide")
}

const writeIcon = (path, size) =>
  sharp(sourcePath)
    .rotate()
    .resize(size, size, { fit: "cover", kernel: sharp.kernel.lanczos3 })
    .toColorspace("srgb")
    .png({ compressionLevel: 9, adaptiveFiltering: true })
    .toFile(path)

for (const [name, size] of [
  ["reflow-32.png", 32],
  ["reflow-180.png", 180],
  ["reflow-192.png", 192],
  ["reflow-512.png", 512],
  ["reflow-maskable-512.png", 512],
]) {
  await writeIcon(resolve(outputDir, name), size)
}

await writeIcon(resolve(nativeOutputDir, "ReFlow-1024.png"), 1024)

// The source art carries an opaque near-white field with ~20% padding around the mark, which is
// right for OS tiles but leaves the glyph ~25px inside a 42px sidebar frame. These variants drop
// the background and trim to the mark so small UI surfaces keep the same optical weight.
const transparentSource = async () => {
  const { data, info } = await sharp(sourcePath).raw().toBuffer({ resolveWithObject: true })
  const { width, height, channels } = info
  const rgba = Buffer.alloc(width * height * 4)
  let minX = width,
    maxX = -1,
    minY = height,
    maxY = -1
  for (let i = 0, o = 0; i < width * height; i++, o += 4) {
    const r = data[i * channels],
      g = data[i * channels + 1],
      b = data[i * channels + 2]
    const luminance = (r + g + b) / 3
    const alpha =
      luminance <= 232 ? 255 : luminance >= 250 ? 0 : Math.round((255 * (250 - luminance)) / 18)
    rgba[o] = r
    rgba[o + 1] = g
    rgba[o + 2] = b
    rgba[o + 3] = alpha
    if (alpha > 20) {
      const x = i % width,
        y = Math.floor(i / width)
      if (x < minX) minX = x
      if (x > maxX) maxX = x
      if (y < minY) minY = y
      if (y > maxY) maxY = y
    }
  }
  const box = Math.max(maxX - minX + 1, maxY - minY + 1)
  const side = box + Math.round(box * 0.04) * 2
  const full = await sharp(rgba, { raw: { width, height, channels: 4 } })
    .png()
    .toBuffer()
  return sharp(full).extract({
    left: Math.round(minX - (side - (maxX - minX + 1)) / 2),
    top: Math.round(minY - (side - (maxY - minY + 1)) / 2),
    width: side,
    height: side,
  })
}

const mark = await transparentSource()
for (const [name, size] of [
  ["reflow-mark-32.png", 32],
  ["reflow-mark-192.png", 192],
]) {
  await mark
    .clone()
    .resize(size, size, { kernel: sharp.kernel.lanczos3 })
    .toColorspace("srgb")
    .png({ compressionLevel: 9, adaptiveFiltering: true })
    .toFile(resolve(outputDir, name))
}
