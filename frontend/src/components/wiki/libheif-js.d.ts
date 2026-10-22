// The slice of libheif-js's wasm bundle the HEIC worker uses. The package's
// own libheif.d.ts describes the low-level embind module, not the decoder
// wrapper in front of it.
declare module "libheif-js/libheif-wasm/libheif-bundle.mjs" {
  interface HeifImage {
    get_width(): number
    get_height(): number
    display(
      target: { data: Uint8ClampedArray; width: number; height: number },
      callback: (result: { data: Uint8ClampedArray; width: number; height: number } | null) => void,
    ): void
    free(): void
  }
  interface HeifDecoder {
    decode(bytes: Uint8Array | ArrayBuffer): HeifImage[]
  }
  interface LibHeif {
    HeifDecoder: new () => HeifDecoder
  }
  const factory: () => LibHeif
  export default factory
}
