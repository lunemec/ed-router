use rkyv::{Archive, Archived, Deserialize, Infallible, Serialize};
use rstar::Point;
use serde::Deserialize as SerdeDeserialize;
use serde::Serialize as SerdeSerialize;

// --- R-Tree Data Structures ---

#[derive(Archive, Deserialize, SerdeDeserialize, Serialize, Debug, Clone, PartialEq)]
#[archive(check_bytes)]
#[archive_attr(derive(Debug))]
pub struct SystemPoint {
    pub id: i64,
    pub idx: usize,
    pub x: f64,
    pub y: f64,
    pub z: f64,
    // Flags for the PRIMARY (arrival) star. Distance is implicitly 0 ls.
    pub flags: u8,
    // A list of other relevant stars in the system.
    // Most systems will have an empty vec here, saving memory.
    pub secondary_stars: Vec<SecondaryStar>,
}

#[derive(Archive, Deserialize, SerdeDeserialize, Serialize, Debug, Clone, Copy, PartialEq)]
#[archive(check_bytes)]
#[archive_attr(derive(Debug))]
pub struct SecondaryStar {
    pub distance_ls: f32,
    pub flags: u8,
}

pub trait SystemPointExt {
    fn get_closest_scoopable_ls(&self) -> Option<f32>;
    fn get_closest_neutron_ls(&self) -> Option<f32>;
    fn get_closest_white_dwarf_ls(&self) -> Option<f32>;
}

macro_rules! impl_system_point_ext {
    ($type:ty) => {
        impl SystemPointExt for $type {
            fn get_closest_scoopable_ls(&self) -> Option<f32> {
                let mut closest_dist = if (self.flags & StarFlags::Scoopable as u8) != 0 {
                    0.0
                } else {
                    f32::MAX
                };
                for star in self.secondary_stars.iter() {
                    if (star.flags & StarFlags::Scoopable as u8) != 0
                        && star.distance_ls < closest_dist
                    {
                        closest_dist = star.distance_ls;
                    }
                }
                if closest_dist == f32::MAX {
                    None
                } else {
                    Some(closest_dist)
                }
            }

            fn get_closest_neutron_ls(&self) -> Option<f32> {
                let mut closest_dist = if (self.flags & StarFlags::Neutron as u8) != 0 {
                    0.0
                } else {
                    f32::MAX
                };
                for star in self.secondary_stars.iter() {
                    if (star.flags & StarFlags::Neutron as u8) != 0
                        && star.distance_ls < closest_dist
                    {
                        closest_dist = star.distance_ls;
                    }
                }
                if closest_dist == f32::MAX {
                    None
                } else {
                    Some(closest_dist)
                }
            }

            fn get_closest_white_dwarf_ls(&self) -> Option<f32> {
                let mut closest_dist = if (self.flags & StarFlags::WhiteDwarf as u8) != 0 {
                    0.0
                } else {
                    f32::MAX
                };
                for star in self.secondary_stars.iter() {
                    if (star.flags & StarFlags::WhiteDwarf as u8) != 0
                        && star.distance_ls < closest_dist
                    {
                        closest_dist = star.distance_ls;
                    }
                }
                if closest_dist == f32::MAX {
                    None
                } else {
                    Some(closest_dist)
                }
            }
        }
    };
}

impl_system_point_ext!(SystemPoint);
impl_system_point_ext!(Archived<SystemPoint>);

impl Point for SystemPoint {
    type Scalar = f64;
    const DIMENSIONS: usize = 3;

    fn generate(mut generator: impl FnMut(usize) -> Self::Scalar) -> Self {
        SystemPoint {
            id: 0,
            idx: 0,
            x: generator(0),
            y: generator(1),
            z: generator(2),
            flags: 0,
            secondary_stars: vec![],
        }
    }
    fn nth(&self, index: usize) -> Self::Scalar {
        match index {
            0 => self.x,
            1 => self.y,
            2 => self.z,
            _ => unreachable!(),
        }
    }
    fn nth_mut(&mut self, index: usize) -> &mut Self::Scalar {
        match index {
            0 => &mut self.x,
            1 => &mut self.y,
            2 => &mut self.z,
            _ => unreachable!(),
        }
    }
}

// Bitflags for star properties
#[derive(Clone, Copy)]
#[repr(u8)]
pub enum StarFlags {
    Scoopable = 1 << 0,
    Neutron = 1 << 1,
    WhiteDwarf = 1 << 2,
}

// --- Top-level struct for our cache file ---

#[derive(Archive, Deserialize, Serialize, Debug, Default)]
#[archive(check_bytes)]
pub struct CacheStats {
    pub scoopable_count: u64,
    pub neutron_count: u64,
    pub wd_count: u64,
}

#[derive(Archive, Deserialize, Serialize, Debug)]
#[archive(check_bytes)]
pub struct FullCacheData {
    pub points: Vec<SystemPoint>,
    pub stats: CacheStats,
}

// --- Structs for API communication (using serde) ---

#[derive(SerdeDeserialize)]
pub struct SearchQuery {
    pub name: String,
}

#[derive(SerdeSerialize)]
pub struct SystemSearchResult {
    pub id: i64,
    pub name: String,
}

pub fn get_system_by_idx(idx: usize, points: &[Archived<SystemPoint>]) -> Option<SystemPoint> {
    let archived_system = points.get(idx)?;
    Some(archived_system.deserialize(&mut Infallible).unwrap())
}
