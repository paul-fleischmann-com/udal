/* Generic Cortex-M4 memory layout (STM32F401-ish sizing) — this is a
 * synthetic size-check image, not firmware for a real board, so the exact
 * numbers only need to be large enough that the linker doesn't reject the
 * (tiny) actual image. */
MEMORY
{
  FLASH : ORIGIN = 0x08000000, LENGTH = 512K
  RAM : ORIGIN = 0x20000000, LENGTH = 96K
}
