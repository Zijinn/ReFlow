export type CanvasPhotoErrorCode = "unsupported" | "decode" | "too-large"

export class CanvasPhotoError extends Error {
  readonly code: CanvasPhotoErrorCode

  constructor(code: CanvasPhotoErrorCode, message: string) {
    super(message)
    this.name = "CanvasPhotoError"
    this.code = code
  }
}

// 这张图最终只当画布底色用：CSS 那边 `background-size: cover` 会把它拉满整个窗口，
// 所以这里的长边上限实际决定"放大多少倍"。用户的主力窗口是 2960 设备像素宽，1600
// 在那儿要放大 1.85 倍，边缘和纹理直接糊成一片——这正是"图片还是不够清晰"的另一半
// 原因（另一半是蒙纱地板，已在 styles.css 里降到拉轴下限）。2560 覆盖到外接屏短边，
// 放大约 1.16 倍，肉眼几乎看不出来。
// 但纱变薄意味着照片颗粒也一起露出来：JPEG 这一道重编码仍然是主要的低通，所以下面
// 的阶梯先降质量、后降尺寸——糊成马赛克比少 500 像素更难看。
const MAX_EDGE = 2560
// 存的是 data URL，配额只有 ~5MB，还要给标注等数据留空间。
const MAX_DATA_URL_LENGTH = 2_000_000

// 超配额不再直接报错，而是按这张表从"最清楚"往"最省字节"退。2560 长边的写实照片在
// 0.82 下通常落在 0.6–1.3M 字符，第一档大多就能过；退到最后一档还装不下才抛 too-large，
// 因为"这张图存不下"对用户来说比任何画质妥协都更莫名其妙。
const ENCODE_LADDER = [
  { edge: MAX_EDGE, quality: 0.82 },
  { edge: MAX_EDGE, quality: 0.72 },
  { edge: 2000, quality: 0.72 },
  { edge: 1600, quality: 0.7 },
]

interface DecodedSource {
  width: number
  height: number
  paint: (context: CanvasRenderingContext2D, width: number, height: number) => void
  dispose: () => void
}

function scaleToFit(width: number, height: number, maxEdge: number) {
  const longest = Math.max(width, height)
  // 小图保持原尺寸：放大只会让 data URL 变大，画质不会变好。
  if (longest <= maxEdge) return { width, height }
  const scale = maxEdge / longest
  return {
    width: Math.max(1, Math.round(width * scale)),
    height: Math.max(1, Math.round(height * scale)),
  }
}

function loadImage(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const image = new Image()
    image.onload = () => resolve(image)
    image.onerror = () =>
      reject(new CanvasPhotoError("decode", "Could not decode the canvas photo."))
    image.src = url
  })
}

async function decodeViaObjectURL(file: File): Promise<DecodedSource> {
  const url = URL.createObjectURL(file)
  let source: DecodedSource | null = null
  try {
    const image = await loadImage(url)
    source = {
      width: image.naturalWidth,
      height: image.naturalHeight,
      paint: (context, width, height) => context.drawImage(image, 0, 0, width, height),
      dispose: () => URL.revokeObjectURL(url),
    }
    return source
  } finally {
    // 解码失败时 dispose 还没交出去，object URL 必须在这里还掉，否则 blob 会一直挂在内存里。
    if (!source) URL.revokeObjectURL(url)
  }
}

async function decode(file: File): Promise<DecodedSource> {
  if (typeof createImageBitmap === "function") {
    try {
      const bitmap = await createImageBitmap(file)
      return {
        width: bitmap.width,
        height: bitmap.height,
        paint: (context, width, height) => context.drawImage(bitmap, 0, 0, width, height),
        dispose: () => bitmap.close(),
      }
    } catch {
      // MIME 写着 image/* 也可能解不出来（HEIC、畸形文件），回退到 <img> 再试一次。
    }
  }
  return decodeViaObjectURL(file)
}

function encode(source: DecodedSource, edge: number, quality: number): string {
  const { width, height } = scaleToFit(source.width, source.height, edge)
  const canvas = document.createElement("canvas")
  canvas.width = width
  canvas.height = height
  const context = canvas.getContext("2d")
  if (!context) throw new CanvasPhotoError("decode", "Canvas 2d context is unavailable.")
  // JPEG 没有 alpha 通道，透明 PNG 直接画会压成黑底，先铺一层白。
  context.fillStyle = "#ffffff"
  context.fillRect(0, 0, width, height)
  source.paint(context, width, height)
  return canvas.toDataURL("image/jpeg", quality)
}

export async function readCanvasPhoto(file: File): Promise<string> {
  if (!file.type.startsWith("image/")) {
    throw new CanvasPhotoError("unsupported", `Not an image file: ${file.type || "unknown type"}`)
  }
  const source = await decode(file)
  try {
    if (source.width < 1 || source.height < 1) {
      throw new CanvasPhotoError("decode", "Canvas photo has no pixels.")
    }
    for (const rung of ENCODE_LADDER) {
      const dataURL = encode(source, rung.edge, rung.quality)
      if (dataURL.length <= MAX_DATA_URL_LENGTH) return dataURL
    }
    throw new CanvasPhotoError("too-large", "Canvas photo exceeds the storage quota.")
  } finally {
    source.dispose()
  }
}
