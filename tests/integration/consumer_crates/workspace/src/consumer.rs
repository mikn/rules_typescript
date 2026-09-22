fn main() {
    cfg_if::cfg_if! {
        if #[cfg(debug_assertions)] {
            println!("consumer debug");
        } else {
            println!("consumer release");
        }
    }
}
