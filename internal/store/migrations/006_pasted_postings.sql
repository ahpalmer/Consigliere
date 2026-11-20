-- When the owner pasted a posting's full text over what its source gave (M11: alert emails carry only a snippet).
-- A pasted posting keeps its text when the source reports the item again.
ALTER TABLE "RadarItems" ADD COLUMN "PastedAt" TEXT;
