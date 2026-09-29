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
// 所以这里的 480 不是尺寸上限而是模糊半径——长边压到 480，放大时的双线性插值就是
// 一次高斯式模糊。留高频细节没有意义：画布上压的是有地板的蒙纱（styles.css 实测浅档
// 87% / 深档 88% 才守住 11px 三级墨 AA），亮度带宽只剩几十级，照片的颗粒只会变成
// 文字底下的噪点，而大色块和走向还留在。附带好处：data URL 只剩几十 KB。
const MAX_EDGE = 480
const JPEG_QUALITY = 0.82
// 存的是 data URL，配额只有 ~5MB，还要给标注等数据留空间。
const MAX_DATA_URL_LENGTH = 2_000_000

interface DecodedSource {
  width: number
  height: number
  paint: (context: CanvasRenderingContext2D, width: number, height: number) => void
  dispose: () => void
}

function scaleToFit(width: number, height: number) {
  const longest = Math.max(width, height)
  // 小图保持原尺寸：放大只会让 data URL 变大，画质不会变好。
  if (longest <= MAX_EDGE) return { width, height }
  const scale = MAX_EDGE / longest
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

export async function readCanvasPhoto(file: File): Promise<string> {
  if (!file.type.startsWith("image/")) {
    throw new CanvasPhotoError("unsupported", `Not an image file: ${file.type || "unknown type"}`)
  }
  const source = await decode(file)
  try {
    if (source.width < 1 || source.height < 1) {
      throw new CanvasPhotoError("decode", "Canvas photo has no pixels.")
    }
    const { width, height } = scaleToFit(source.width, source.height)
    const canvas = document.createElement("canvas")
    canvas.width = width
    canvas.height = height
    const context = canvas.getContext("2d")
    if (!context) throw new CanvasPhotoError("decode", "Canvas 2d context is unavailable.")
    // JPEG 没有 alpha 通道，透明 PNG 直接画会压成黑底，先铺一层白。
    context.fillStyle = "#ffffff"
    context.fillRect(0, 0, width, height)
    source.paint(context, width, height)
    const dataURL = canvas.toDataURL("image/jpeg", JPEG_QUALITY)
    if (dataURL.length > MAX_DATA_URL_LENGTH) {
      throw new CanvasPhotoError("too-large", "Canvas photo exceeds the storage quota.")
    }
    return dataURL
  } finally {
    source.dispose()
  }
}
