#include <stdio.h>

static int decode_target(int value) {
  puts("CAT48 target decoder");
  return value + 48;
}

int main(void) {
  return decode_target(2) == 50 ? 0 : 1;
}
